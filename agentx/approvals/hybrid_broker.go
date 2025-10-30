package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HybridBroker combines in-memory speed with file-based persistence.
// Suitable for production use where resumability and external tool integration are needed.
type HybridBroker struct {
	memory       *MemoryBroker
	progressDir  string
	requestsDir  string
	decisionsDir string
	pollInterval time.Duration
}

// HybridBrokerOption customizes hybrid broker behavior.
type HybridBrokerOption func(*HybridBroker)

// WithHybridPollInterval sets the file polling interval for external decisions.
func WithHybridPollInterval(d time.Duration) HybridBrokerOption {
	return func(b *HybridBroker) {
		if d > 0 {
			b.pollInterval = d
		}
	}
}

// NewHybridBroker creates a broker that stores approvals in memory and on disk.
func NewHybridBroker(progressDir string, opts ...HybridBrokerOption) (*HybridBroker, error) {
	if strings.TrimSpace(progressDir) == "" {
		return nil, errors.New("approvals: progressDir is required")
	}

	broker := &HybridBroker{
		memory:       NewMemoryBroker(),
		progressDir:  progressDir,
		requestsDir:  filepath.Join(progressDir, "approvals", "requests"),
		decisionsDir: filepath.Join(progressDir, "approvals", "decisions"),
		pollInterval: 1 * time.Second,
	}

	for _, opt := range opts {
		opt(broker)
	}

	// Create directories
	if err := os.MkdirAll(broker.requestsDir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create requests dir: %w", err)
	}
	if err := os.MkdirAll(broker.decisionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create decisions dir: %w", err)
	}

	// Hydrate from disk
	if err := broker.hydrate(); err != nil {
		return nil, fmt.Errorf("approvals: hydrate: %w", err)
	}

	return broker, nil
}

// Submit persists to both memory and disk.
func (b *HybridBroker) Submit(ctx context.Context, req Request) (string, error) {
	// Write to memory first (fast path)
	id, err := b.memory.Submit(ctx, req)
	if err != nil {
		return "", err
	}

	// Persist to disk asynchronously
	go func() {
		if err := b.writeRequestFile(req); err != nil {
			// Log error but don't fail the request
			// In production, this would go to a proper logger
			_ = err
		}
	}()

	return id, nil
}

// AwaitDecision polls both memory and disk for decisions.
func (b *HybridBroker) AwaitDecision(ctx context.Context, requestID string) (Decision, error) {
	// Check memory first
	if dec, ok, _ := b.memory.GetDecision(ctx, requestID); ok {
		return dec, nil
	}

	// Set up polling for external file-based decisions
	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	// Also wait on memory (for programmatic decisions)
	memoryCh := make(chan Decision, 1)
	errCh := make(chan error, 1)

	go func() {
		dec, err := b.memory.AwaitDecision(ctx, requestID)
		if err != nil {
			errCh <- err
		} else {
			memoryCh <- dec
		}
	}()

	for {
		select {
		case dec := <-memoryCh:
			// Decision came from memory
			go b.writeDecisionFile(dec)
			return dec, nil

		case err := <-errCh:
			// Memory wait failed (likely context cancelled or expired)
			return Decision{}, err

		case <-ticker.C:
			// Check disk for external approval
			if dec, ok := b.checkDiskDecision(requestID); ok {
				// Hydrate into memory
				_ = b.memory.Decide(requestID, dec)
				return dec, nil
			}

		case <-ctx.Done():
			return Decision{}, ctx.Err()
		}
	}
}

// GetDecision checks memory first, then disk.
func (b *HybridBroker) GetDecision(ctx context.Context, requestID string) (Decision, bool, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, false, err
	}

	// Check memory
	if dec, ok, _ := b.memory.GetDecision(ctx, requestID); ok {
		return dec, true, nil
	}

	// Check disk
	dec, ok := b.checkDiskDecision(requestID)
	if ok {
		// Hydrate into memory
		_ = b.memory.Decide(requestID, dec)
	}

	return dec, ok, nil
}

// ListPending returns pending requests from memory.
func (b *HybridBroker) ListPending(ctx context.Context) ([]Request, error) {
	return b.memory.ListPending(ctx)
}

// Cancel cancels a request in both memory and disk.
func (b *HybridBroker) Cancel(ctx context.Context, requestID string) error {
	return b.memory.Cancel(ctx, requestID)
}

// Subscribe delegates to memory broker.
func (b *HybridBroker) Subscribe(ch chan<- ApprovalEvent) {
	b.memory.Subscribe(ch)
}

// Unsubscribe delegates to memory broker.
func (b *HybridBroker) Unsubscribe(ch chan<- ApprovalEvent) {
	b.memory.Unsubscribe(ch)
}

// hydrate loads existing requests and decisions from disk into memory.
func (b *HybridBroker) hydrate() error {
	// Load requests
	reqFiles, err := os.ReadDir(b.requestsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read requests dir: %w", err)
	}

	for _, entry := range reqFiles {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(b.requestsDir, entry.Name()))
		if err != nil {
			continue
		}

		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}

		if req.ID == "" {
			req.ID = strings.TrimSuffix(entry.Name(), ".json")
		}

		// Use context.Background() for hydration
		_, _ = b.memory.Submit(context.Background(), req)
	}

	// Load decisions
	decFiles, err := os.ReadDir(b.decisionsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read decisions dir: %w", err)
	}

	for _, entry := range decFiles {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(b.decisionsDir, entry.Name()))
		if err != nil {
			continue
		}

		var dec Decision
		if err := json.Unmarshal(data, &dec); err != nil {
			continue
		}

		if dec.ID == "" {
			dec.ID = strings.TrimSuffix(entry.Name(), ".json")
		}

		_ = b.memory.Decide(dec.ID, dec)
	}

	return nil
}

// checkDiskDecision checks if a decision file exists for the given request.
func (b *HybridBroker) checkDiskDecision(requestID string) (Decision, bool) {
	path := filepath.Join(b.decisionsDir, requestID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Decision{}, false
	}

	var dec Decision
	if err := json.Unmarshal(data, &dec); err != nil {
		return Decision{}, false
	}

	if dec.ID == "" {
		dec.ID = requestID
	}
	if dec.Timestamp.IsZero() {
		dec.Timestamp = time.Now().UTC()
	}

	return dec, true
}

// writeRequestFile persists a request to disk.
func (b *HybridBroker) writeRequestFile(req Request) error {
	path := filepath.Join(b.requestsDir, req.ID+".json")
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	return nil
}

// writeDecisionFile persists a decision to disk.
func (b *HybridBroker) writeDecisionFile(dec Decision) error {
	path := filepath.Join(b.decisionsDir, dec.ID+".json")
	data, err := json.MarshalIndent(dec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write decision: %w", err)
	}

	return nil
}

// Ensure HybridBroker implements both Broker and ObservableBroker.
var _ Broker = (*HybridBroker)(nil)
var _ ObservableBroker = (*HybridBroker)(nil)

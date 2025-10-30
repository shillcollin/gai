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

	"github.com/shillcollin/gai/agentx/internal/id"
	"github.com/shillcollin/gai/agentx/plan"
)

// FileBroker persists approvals using the on-disk schema described in docs/AGENT.md.
type FileBroker struct {
	progressDir  string
	requestsDir  string
	decisionsDir string
	pollInterval time.Duration
}

// FileBrokerOption customises broker behaviour.
type FileBrokerOption func(*FileBroker)

// WithPollInterval overrides the default polling interval for AwaitDecision.
func WithPollInterval(d time.Duration) FileBrokerOption {
	return func(b *FileBroker) {
		if d > 0 {
			b.pollInterval = d
		}
	}
}

// NewFileBroker constructs a broker rooted at progressDir.
func NewFileBroker(progressDir string, opts ...FileBrokerOption) (*FileBroker, error) {
	if strings.TrimSpace(progressDir) == "" {
		return nil, errors.New("approvals: progressDir is required")
	}
	broker := &FileBroker{
		progressDir:  progressDir,
		requestsDir:  filepath.Join(progressDir, "approvals", "requests"),
		decisionsDir: filepath.Join(progressDir, "approvals", "decisions"),
		pollInterval: 500 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(broker)
	}
	if err := os.MkdirAll(broker.requestsDir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create requests dir: %w", err)
	}
	if err := os.MkdirAll(broker.decisionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create decisions dir: %w", err)
	}
	return broker, nil
}

// Submit writes the request to disk and returns the request ID.
func (b *FileBroker) Submit(ctx context.Context, req Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	normalized, err := b.normaliseRequest(req)
	if err != nil {
		return "", err
	}

	path := b.requestPath(normalized.ID)

	payload, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return "", fmt.Errorf("approvals: marshal request: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", fmt.Errorf("approvals: write request: %w", err)
	}
	return normalized.ID, nil
}

// AwaitDecision blocks until a decision is available or the request expires.
func (b *FileBroker) AwaitDecision(ctx context.Context, id string) (Decision, error) {
	req, err := b.readRequest(id)
	if err != nil {
		return Decision{}, err
	}

	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}

		if dec, ok, err := b.GetDecision(ctx, id); err != nil {
			if errors.Is(err, ErrStalePlan) {
				return Decision{}, err
			}
			return Decision{}, err
		} else if ok {
			return dec, nil
		}

		if !req.ExpiresAt.IsZero() && time.Now().After(req.ExpiresAt) {
			dec := Decision{
				ID:        id,
				Status:    StatusExpired,
				Timestamp: time.Now().UTC(),
			}
			if err := b.writeDecision(dec); err != nil {
				return Decision{}, err
			}
			return dec, ErrExpired
		}

		select {
		case <-ctx.Done():
			return Decision{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// GetDecision returns an existing decision if recorded.
func (b *FileBroker) GetDecision(ctx context.Context, id string) (Decision, bool, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, false, err
	}
	path := b.decisionPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Decision{}, false, nil
		}
		return Decision{}, false, fmt.Errorf("approvals: read decision: %w", err)
	}

	var dec Decision
	if err := json.Unmarshal(data, &dec); err != nil {
		return Decision{}, false, fmt.Errorf("approvals: decode decision: %w", err)
	}
	if dec.ID == "" {
		dec.ID = id
	}
	if dec.Timestamp.IsZero() {
		dec.Timestamp = time.Now().UTC()
	}

	if err := b.validateDecision(dec); err != nil {
		return Decision{}, false, err
	}

	req, err := b.readRequest(id)
	if err != nil {
		return Decision{}, false, err
	}
	if req.Kind == KindPlan && req.PlanSig != "" {
		current, err := b.currentPlanSignature(req)
		if err != nil {
			return Decision{}, false, err
		}
		if current != req.PlanSig {
			return Decision{}, false, ErrStalePlan
		}
	}

	return dec, true, nil
}

func (b *FileBroker) normaliseRequest(req Request) (Request, error) {
	if req.Kind == "" {
		return Request{}, errors.New("approvals: kind is required")
	}
	if req.Kind != KindPlan && req.Kind != KindTool {
		return Request{}, fmt.Errorf("approvals: unsupported kind %q", req.Kind)
	}
	if req.Kind == KindPlan {
		if strings.TrimSpace(req.PlanPath) == "" {
			return Request{}, errors.New("approvals: plan_path is required for plan approvals")
		}
		if strings.TrimSpace(req.PlanSig) == "" {
			return Request{}, errors.New("approvals: plan_sig is required for plan approvals")
		}
	}
	if req.Kind == KindTool && strings.TrimSpace(req.ToolName) == "" {
		return Request{}, errors.New("approvals: tool_name is required for tool approvals")
	}
	if strings.TrimSpace(req.ID) == "" {
		generated, err := id.New()
		if err != nil {
			return Request{}, err
		}
		req.ID = generated
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	if !req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.ExpiresAt.UTC()
	}
	return req, nil
}

func (b *FileBroker) validateDecision(dec Decision) error {
	if dec.Status == "" {
		return errors.New("approvals: decision missing status")
	}
	switch dec.Status {
	case StatusApproved, StatusDenied, StatusExpired:
	default:
		return fmt.Errorf("approvals: unsupported decision status %q", dec.Status)
	}
	if dec.Timestamp.IsZero() {
		return errors.New("approvals: decision missing timestamp")
	}
	return nil
}

func (b *FileBroker) currentPlanSignature(req Request) (string, error) {
	if req.PlanPath == "" {
		return "", nil
	}
	abs := req.PlanPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(b.progressDir, req.PlanPath)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("approvals: read plan: %w", err)
	}
	var p plan.PlanV1
	if err := json.Unmarshal(data, &p); err != nil {
		return "", fmt.Errorf("approvals: decode plan: %w", err)
	}
	sig, err := p.Signature()
	if err != nil {
		return "", err
	}
	return sig, nil
}

func (b *FileBroker) writeDecision(dec Decision) error {
	payload, err := json.MarshalIndent(dec, "", "  ")
	if err != nil {
		return fmt.Errorf("approvals: marshal decision: %w", err)
	}
	if err := os.WriteFile(b.decisionPath(dec.ID), payload, 0o644); err != nil {
		return fmt.Errorf("approvals: write decision: %w", err)
	}
	return nil
}

func (b *FileBroker) readRequest(id string) (Request, error) {
	path := b.requestPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, fmt.Errorf("approvals: read request: %w", err)
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return Request{}, fmt.Errorf("approvals: decode request: %w", err)
	}
	req.ID = id
	return req, nil
}

func (b *FileBroker) requestPath(id string) string {
	return filepath.Join(b.requestsDir, id+".json")
}

func (b *FileBroker) decisionPath(id string) string {
	return filepath.Join(b.decisionsDir, id+".json")
}

package approvals

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shillcollin/gai/agentx/internal/id"
)

// MemoryBroker implements ApprovalBroker using in-memory storage.
// Fast, suitable for testing and single-process scenarios.
type MemoryBroker struct {
	mu        sync.RWMutex
	requests  map[string]Request
	decisions map[string]Decision
	waiters   map[string][]chan Decision
	subs      []chan<- ApprovalEvent
}

// ApprovalEvent represents an approval lifecycle event.
type ApprovalEvent struct {
	Type      string    // "requested", "decided", "expired", "cancelled"
	RequestID string
	Request   Request
	Decision  Decision
	Timestamp int64
}

// NewMemoryBroker creates a new in-memory approval broker.
func NewMemoryBroker() *MemoryBroker {
	return &MemoryBroker{
		requests:  make(map[string]Request),
		decisions: make(map[string]Decision),
		waiters:   make(map[string][]chan Decision),
		subs:      make([]chan<- ApprovalEvent, 0),
	}
}

// Submit writes a request to memory and notifies subscribers.
func (b *MemoryBroker) Submit(ctx context.Context, req Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if req.ID == "" {
		generated, err := id.New()
		if err != nil {
			return "", fmt.Errorf("approvals: generate id: %w", err)
		}
		req.ID = generated
	}

	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}

	if !req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.ExpiresAt.UTC()
	}

	b.requests[req.ID] = req

	// Notify subscribers
	b.notifyLocked(ApprovalEvent{
		Type:      "requested",
		RequestID: req.ID,
		Request:   req,
		Timestamp: time.Now().UnixMilli(),
	})

	return req.ID, nil
}

// AwaitDecision blocks until a decision is available or context is cancelled.
func (b *MemoryBroker) AwaitDecision(ctx context.Context, requestID string) (Decision, error) {
	b.mu.Lock()

	// Check if already decided
	if dec, ok := b.decisions[requestID]; ok {
		b.mu.Unlock()
		return dec, nil
	}

	// Get request for expiration checking
	req, reqExists := b.requests[requestID]
	if !reqExists {
		b.mu.Unlock()
		return Decision{}, fmt.Errorf("approvals: request %s not found", requestID)
	}

	// Create waiter channel
	ch := make(chan Decision, 1)
	b.waiters[requestID] = append(b.waiters[requestID], ch)
	b.mu.Unlock()

	// Set up expiration timer if needed
	var expireTimer *time.Timer
	var expireCh <-chan time.Time
	if !req.ExpiresAt.IsZero() {
		expireDuration := time.Until(req.ExpiresAt)
		if expireDuration > 0 {
			expireTimer = time.NewTimer(expireDuration)
			expireCh = expireTimer.C
		}
	}
	if expireTimer != nil {
		defer expireTimer.Stop()
	}

	// Wait for decision, expiration, or cancellation
	select {
	case dec := <-ch:
		return dec, nil

	case <-expireCh:
		// Auto-expire
		expiredDec := Decision{
			ID:        requestID,
			Status:    StatusExpired,
			Timestamp: time.Now().UTC(),
		}
		_ = b.Decide(requestID, expiredDec)
		return expiredDec, ErrExpired

	case <-ctx.Done():
		// Clean up waiter
		b.mu.Lock()
		b.removeWaiterLocked(requestID, ch)
		b.mu.Unlock()
		return Decision{}, ctx.Err()
	}
}

// GetDecision retrieves a decision without blocking.
func (b *MemoryBroker) GetDecision(ctx context.Context, requestID string) (Decision, bool, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, false, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	dec, ok := b.decisions[requestID]
	return dec, ok, nil
}

// ListPending returns all requests that have not been decided.
func (b *MemoryBroker) ListPending(ctx context.Context) ([]Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	var pending []Request
	for id, req := range b.requests {
		if _, decided := b.decisions[id]; !decided {
			pending = append(pending, req)
		}
	}

	return pending, nil
}

// Cancel cancels a pending request.
func (b *MemoryBroker) Cancel(ctx context.Context, requestID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	cancelledDec := Decision{
		ID:        requestID,
		Status:    StatusDenied,
		Reason:    "cancelled",
		Timestamp: time.Now().UTC(),
	}

	return b.Decide(requestID, cancelledDec)
}

// Decide records a decision and notifies waiters.
func (b *MemoryBroker) Decide(requestID string, decision Decision) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.requests[requestID]; !exists {
		return fmt.Errorf("approvals: request %s not found", requestID)
	}

	decision.ID = requestID
	if decision.Timestamp.IsZero() {
		decision.Timestamp = time.Now().UTC()
	}

	b.decisions[requestID] = decision

	// Wake up all waiters for this request
	for _, ch := range b.waiters[requestID] {
		select {
		case ch <- decision:
		default:
			// Waiter already gone, ignore
		}
	}
	delete(b.waiters, requestID)

	// Notify subscribers
	b.notifyLocked(ApprovalEvent{
		Type:      "decided",
		RequestID: requestID,
		Decision:  decision,
		Timestamp: time.Now().UnixMilli(),
	})

	return nil
}

// Subscribe registers a channel to receive approval events.
func (b *MemoryBroker) Subscribe(ch chan<- ApprovalEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, ch)
}

// Unsubscribe removes a channel from event notifications.
func (b *MemoryBroker) Unsubscribe(ch chan<- ApprovalEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.subs {
		if sub == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
}

// notifyLocked sends an event to all subscribers (caller must hold lock).
func (b *MemoryBroker) notifyLocked(ev ApprovalEvent) {
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Don't block if subscriber is slow
		}
	}
}

// removeWaiterLocked removes a specific waiter channel (caller must hold lock).
func (b *MemoryBroker) removeWaiterLocked(requestID string, ch chan Decision) {
	waiters := b.waiters[requestID]
	for i, w := range waiters {
		if w == ch {
			b.waiters[requestID] = append(waiters[:i], waiters[i+1:]...)
			if len(b.waiters[requestID]) == 0 {
				delete(b.waiters, requestID)
			}
			break
		}
	}
}

// ObservableBroker is an optional interface for brokers that support event subscriptions.
type ObservableBroker interface {
	Broker
	Subscribe(ch chan<- ApprovalEvent)
	Unsubscribe(ch chan<- ApprovalEvent)
}

// Ensure MemoryBroker implements both Broker and ObservableBroker.
var _ Broker = (*MemoryBroker)(nil)
var _ ObservableBroker = (*MemoryBroker)(nil)

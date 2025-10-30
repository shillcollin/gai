package approvals

import (
	"context"
	"errors"
	"time"
)

// Kind enumerates the approval type requested.
type Kind string

const (
	KindPlan Kind = "plan"
	KindTool Kind = "tool"
)

// Status represents the outcome of an approval request.
type Status string

const (
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusExpired  Status = "expired"
)

// Request is persisted under progress/approvals/requests/.
type Request struct {
	ID        string         `json:"id"`
	Kind      Kind           `json:"type"`
	PlanPath  string         `json:"plan_path,omitempty"`
	PlanSig   string         `json:"plan_sig,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Rationale string         `json:"rationale,omitempty"`
	ExpiresAt time.Time      `json:"expires_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Decision documents the outcome of a request.
type Decision struct {
	ID         string    `json:"id"`
	Status     Status    `json:"decision"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Reason     string    `json:"reason,omitempty"`
}

// ErrStalePlan indicates that a plan approval decision no longer matches the current plan signature.
var ErrStalePlan = errors.New("approvals: plan signature mismatch")

// ErrExpired indicates the request expired before a decision was recorded.
var ErrExpired = errors.New("approvals: request expired")

// Broker is implemented by approval backends.
type Broker interface {
	Submit(ctx context.Context, req Request) (string, error)
	AwaitDecision(ctx context.Context, id string) (Decision, error)
	GetDecision(ctx context.Context, id string) (Decision, bool, error)
}

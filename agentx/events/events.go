package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// SchemaVersionV1 is the canonical schema identifier for agent events.
	SchemaVersionV1 = "gai.agent.events.v1"
)

// Type enumerates event types emitted by the agent.
type Type string

// Standard event types for v1.
const (
	TypePhaseStart        Type = "phase.start"
	TypePhaseFinish       Type = "phase.finish"
	TypePhaseSkipped      Type = "phase.skipped"
	TypeApprovalRequested Type = "approval.requested"
	TypeApprovalDecided   Type = "approval.decided"
	TypeToolCall          Type = "tool.call"
	TypeToolResult        Type = "tool.result"
	TypeUsageDelta        Type = "usage.delta"
	TypeBudgetHit         Type = "budget.hit"
	TypeFinish            Type = "finish"
	TypeError             Type = "error"
)

// AgentEventV1 represents a single agent event frame.
type AgentEventV1 struct {
	Version  string         `json:"version"`
	ID       string         `json:"id"`
	Type     Type           `json:"type"`
	Ts       int64          `json:"ts"`
	Step     string         `json:"step,omitempty"`
	StepKind string         `json:"step_kind,omitempty"`
	Message  string         `json:"message,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Error    string         `json:"error,omitempty"`
	Progress *ProgressInfo  `json:"progress,omitempty"`
}

// ProgressInfo contains progress metadata for UI display.
type ProgressInfo struct {
	LoopName        string   `json:"loop_name"`
	LoopVersion     string   `json:"loop_version"`
	CurrentPhase    string   `json:"current_phase"`
	CompletedPhases []string `json:"completed_phases"`

	// Rich progress metadata (only present if loop declared phases)
	TotalPhases              int     `json:"total_phases,omitempty"`
	CurrentPhaseIndex        int     `json:"current_phase_index,omitempty"`
	CurrentPhaseDescription  string  `json:"current_phase_description,omitempty"`
	PercentComplete          float64 `json:"percent_complete,omitempty"`

	// Budget tracking
	BudgetsRemaining map[string]any `json:"budgets_remaining,omitempty"`
}

// MarshalJSON ensures the version is always present even if omitted by callers.
func (e AgentEventV1) MarshalJSON() ([]byte, error) {
	type alias AgentEventV1
	if e.Version == "" {
		e.Version = SchemaVersionV1
	}
	return json.Marshal(alias(e))
}

// Validate performs structural validation according to the v1 contract.
func (e AgentEventV1) Validate() error {
	if e.Version == "" {
		return errors.New("events: version is required")
	}
	if e.Version != SchemaVersionV1 {
		return fmt.Errorf("events: unsupported version %q", e.Version)
	}
	if strings.TrimSpace(string(e.Type)) == "" {
		return errors.New("events: type is required")
	}
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("events: id is required")
	}
	if e.Ts == 0 {
		return errors.New("events: timestamp is required")
	}
	if e.Ts < 0 {
		return errors.New("events: timestamp cannot be negative")
	}
	return nil
}

// Now creates a timestamp in UTC milliseconds suitable for Ts.
func Now() int64 {
	return time.Now().UTC().UnixMilli()
}

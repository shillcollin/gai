package core

import (
	"context"

	"github.com/shillcollin/gai/schema"
)

// RunnerState captures the mutable state of a multi-step execution.
type RunnerState struct {
	Messages   []Message
	Steps      []Step
	LastStep   *Step
	Usage      Usage
	StopReason StopReason
}

// LastText returns the accumulated text from the last step, if any.
func (s *RunnerState) LastText() string {
	if s == nil || s.LastStep == nil {
		return ""
	}
	return s.LastStep.Text
}

// TotalSteps reports the execution step count.
func (s *RunnerState) TotalSteps() int {
	if s == nil {
		return 0
	}
	return len(s.Steps)
}

// TotalToolCalls counts tool invocations across steps.
func (s *RunnerState) TotalToolCalls() int {
	if s == nil {
		return 0
	}
	total := 0
	for _, step := range s.Steps {
		total += len(step.ToolCalls)
	}
	return total
}

// Step represents a single step during multi-step execution.
type Step struct {
	Number      int
	Text        string
	ToolCalls   []ToolExecution
	Usage       Usage
	DurationMS  int64
	StartedAt   int64
	CompletedAt int64
	Model       string
}

// ToolExecution captures an individual tool invocation and its outcome.
type ToolExecution struct {
	Call       ToolCall
	Result     any
	Error      error
	DurationMS int64
	Retries    int
}

// FinalState is handed to Finalizer callbacks.
type FinalState struct {
	Messages    []Message
	Steps       []Step
	Usage       Usage
	StopReason  StopReason
	LastText    func() string
	TotalTokens func() int
}

// ToolHandle is implemented by tool adapters that expose typed tool metadata to providers.
type ToolHandle interface {
	Name() string
	Description() string
	InputSchema() *schema.Schema
	OutputSchema() *schema.Schema
	Execute(ctx context.Context, input map[string]any, meta ToolMeta) (any, error)
}

// ToolMeta provides metadata for tool execution.
type ToolMeta struct {
	CallID   string
	StepID   int
	TraceID  string
	SpanID   string
	Metadata map[string]any
}

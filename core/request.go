package core

import "context"

// Request represents a single generation request.
type Request struct {
	Model string `json:"model,omitempty"`

	Messages []Message `json:"messages"`

	Temperature float32 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	TopP        float32 `json:"top_p,omitempty"`
	TopK        int     `json:"top_k,omitempty"`

	Stream bool `json:"stream,omitempty"`

	Safety  *SafetyConfig `json:"safety,omitempty"`
	Session *Session      `json:"session,omitempty"`

	Tools           []ToolHandle   `json:"tools,omitempty"`
	ToolChoice      ToolChoice     `json:"tool_choice,omitempty"`
	StopWhen        StopCondition  `json:"-"`
	OnStop          Finalizer      `json:"-"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`

	Context context.Context `json:"-"`
}

// Clone returns a shallow copy of the request with safe map duplication where
// necessary.
func (r Request) Clone() Request {
	clone := r
	if len(r.Messages) > 0 {
		clone.Messages = append([]Message(nil), r.Messages...)
	}
	if r.Metadata != nil {
		clone.Metadata = make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			clone.Metadata[k] = v
		}
	}
	if r.ProviderOptions != nil {
		clone.ProviderOptions = make(map[string]any, len(r.ProviderOptions))
		for k, v := range r.ProviderOptions {
			clone.ProviderOptions[k] = v
		}
	}
	if r.Tools != nil {
		clone.Tools = append([]ToolHandle(nil), r.Tools...)
	}
	return clone
}

// ToolChoice enumerates how the provider should handle tool execution hints.
type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceRequired ToolChoice = "required"
)

// StopCondition determines whether the runner should halt execution.
type StopCondition func(*RunnerState) (bool, StopReason)

// Finalizer runs after execution completes and can mutate the final result.
type Finalizer func(ctx context.Context, state FinalState) (*TextResult, error)

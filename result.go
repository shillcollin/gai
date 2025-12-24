package gai

import (
	"github.com/shillcollin/gai/core"
)

// Result wraps core.TextResult with convenience accessors.
type Result struct {
	inner *core.TextResult

	// Cached values
	toolCalls         []core.ToolCall
	toolCallsComputed bool

	// Voice pipeline data
	transcript string // STT transcription of input audio
	audioData  []byte // TTS synthesis of output text
	audioMIME  string // MIME type of audio data (e.g., "audio/mpeg")
}

// newResult wraps a core.TextResult.
func newResult(r *core.TextResult) *Result {
	return &Result{inner: r}
}

// Text returns the text content from the response.
func (r *Result) Text() string {
	if r.inner == nil {
		return ""
	}
	return r.inner.Text
}

// HasText returns true if the response contains text content.
func (r *Result) HasText() bool {
	return r.Text() != ""
}

// ToolCalls extracts all tool calls from all execution steps.
func (r *Result) ToolCalls() []core.ToolCall {
	if r.toolCallsComputed {
		return r.toolCalls
	}

	r.toolCallsComputed = true
	if r.inner == nil || len(r.inner.Steps) == 0 {
		return nil
	}

	// Collect all tool calls from all steps
	var calls []core.ToolCall
	for _, step := range r.inner.Steps {
		for _, exec := range step.ToolCalls {
			calls = append(calls, exec.Call)
		}
	}

	r.toolCalls = calls
	return calls
}

// HasToolCalls returns true if any tools were called during execution.
func (r *Result) HasToolCalls() bool {
	return len(r.ToolCalls()) > 0
}

// Model returns the model that generated this response.
func (r *Result) Model() string {
	if r.inner == nil {
		return ""
	}
	return r.inner.Model
}

// Provider returns the provider that generated this response.
func (r *Result) Provider() string {
	if r.inner == nil {
		return ""
	}
	return r.inner.Provider
}

// Usage returns token usage information.
func (r *Result) Usage() core.Usage {
	if r.inner == nil {
		return core.Usage{}
	}
	return r.inner.Usage
}

// TotalTokens returns the total token count (convenience accessor).
func (r *Result) TotalTokens() int {
	return r.Usage().TotalTokens
}

// InputTokens returns the input token count (convenience accessor).
func (r *Result) InputTokens() int {
	return r.Usage().InputTokens
}

// OutputTokens returns the output token count (convenience accessor).
func (r *Result) OutputTokens() int {
	return r.Usage().OutputTokens
}

// Steps returns all execution steps (for multi-step agentic runs).
func (r *Result) Steps() []core.Step {
	if r.inner == nil {
		return nil
	}
	return r.inner.Steps
}

// StepCount returns the number of execution steps.
func (r *Result) StepCount() int {
	return len(r.Steps())
}

// FinishReason returns the reason generation stopped.
func (r *Result) FinishReason() core.StopReason {
	if r.inner == nil {
		return core.StopReason{}
	}
	return r.inner.FinishReason
}

// Warnings returns any warnings generated during the request.
func (r *Result) Warnings() []core.Warning {
	if r.inner == nil {
		return nil
	}
	return r.inner.Warnings
}

// HasWarnings returns true if there are any warnings.
func (r *Result) HasWarnings() bool {
	return len(r.Warnings()) > 0
}

// Citations returns source citations if available.
func (r *Result) Citations() []core.Citation {
	if r.inner == nil {
		return nil
	}
	return r.inner.Citations
}

// LatencyMS returns the request latency in milliseconds.
func (r *Result) LatencyMS() int64 {
	if r.inner == nil {
		return 0
	}
	return r.inner.LatencyMS
}

// TTFBMS returns the time to first byte in milliseconds.
func (r *Result) TTFBMS() int64 {
	if r.inner == nil {
		return 0
	}
	return r.inner.TTFBMS
}

// Core returns the underlying core.TextResult for advanced use cases.
func (r *Result) Core() *core.TextResult {
	return r.inner
}

// Messages returns the messages to append to conversation history.
// For a simple text response, this returns a single assistant message.
// For multi-step agentic runs, this includes tool calls and results.
func (r *Result) Messages() []core.Message {
	if r.inner == nil {
		return nil
	}

	// For simple single-step responses, just return the assistant message
	if len(r.inner.Steps) == 0 {
		if r.inner.Text == "" {
			return nil
		}
		return []core.Message{
			core.AssistantMessage(r.inner.Text),
		}
	}

	// For multi-step runs, collect all messages from steps
	var messages []core.Message
	for _, step := range r.inner.Steps {
		// Add assistant message with text and tool calls
		parts := make([]core.Part, 0)
		if step.Text != "" {
			parts = append(parts, core.Text{Text: step.Text})
		}
		for _, tc := range step.ToolCalls {
			parts = append(parts, core.ToolCall{
				ID:    tc.Call.ID,
				Name:  tc.Call.Name,
				Input: tc.Call.Input,
			})
		}
		if len(parts) > 0 {
			messages = append(messages, core.Message{
				Role:  core.Assistant,
				Parts: parts,
			})
		}

		// Add tool results
		for _, tc := range step.ToolCalls {
			var errStr string
			if tc.Error != nil {
				errStr = tc.Error.Error()
			}
			messages = append(messages, core.Message{
				Role: core.User, // Tool results typically come as user messages
				Parts: []core.Part{
					core.ToolResult{
						ID:     tc.Call.ID,
						Name:   tc.Call.Name,
						Result: tc.Result,
						Error:  errStr,
					},
				},
			})
		}
	}

	return messages
}

// Transcript returns the STT transcription of input audio.
// This is populated when the request included audio content and
// an STT provider was used to transcribe it.
func (r *Result) Transcript() string {
	return r.transcript
}

// HasTranscript returns true if audio was transcribed.
func (r *Result) HasTranscript() bool {
	return r.transcript != ""
}

// AudioData returns the TTS-synthesized audio of the response.
// This is populated when the request specified a Voice() for TTS output.
func (r *Result) AudioData() []byte {
	return r.audioData
}

// AudioMIME returns the MIME type of the audio data (e.g., "audio/mpeg").
func (r *Result) AudioMIME() string {
	return r.audioMIME
}

// HasAudio returns true if audio was synthesized.
func (r *Result) HasAudio() bool {
	return len(r.audioData) > 0
}

// SetTranscript sets the transcript (used internally by voice pipeline).
func (r *Result) SetTranscript(transcript string) {
	r.transcript = transcript
}

// SetAudioData sets the audio data (used internally by voice pipeline).
func (r *Result) SetAudioData(data []byte, mimeType string) {
	r.audioData = data
	r.audioMIME = mimeType
}

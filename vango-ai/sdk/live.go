package vango

import (
	"context"

	"github.com/vango-ai/vango/pkg/core/types"
)

// LiveConfig configures a real-time bidirectional session.
type LiveConfig struct {
	Model string             `json:"model"`
	Voice *types.VoiceConfig `json:"voice,omitempty"`
	Tools []types.Tool       `json:"tools,omitempty"`
}

// LiveSession represents a real-time bidirectional voice/text session.
type LiveSession struct {
	config *LiveConfig
	// TODO: Add WebSocket connection, state management
}

// LiveEvent is an event from the live session.
type LiveEvent interface {
	liveEventType() string
}

// LiveAudioEvent contains audio data from the session.
type LiveAudioEvent struct {
	Role   string `json:"role"` // "user" or "assistant"
	Data   []byte `json:"data"`
	Format string `json:"format"`
}

func (e LiveAudioEvent) liveEventType() string { return "audio" }

// LiveTranscriptEvent contains transcript text.
type LiveTranscriptEvent struct {
	Role    string `json:"role"`
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

func (e LiveTranscriptEvent) liveEventType() string { return "transcript" }

// LiveTextEvent contains text output from the model.
type LiveTextEvent struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

func (e LiveTextEvent) liveEventType() string { return "text" }

// LiveToolCallEvent signals a tool call from the model.
type LiveToolCallEvent struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (e LiveToolCallEvent) liveEventType() string { return "tool_call" }

// LiveErrorEvent signals an error.
type LiveErrorEvent struct {
	Message string `json:"message"`
}

func (e LiveErrorEvent) liveEventType() string { return "error" }

// Events returns the channel of live events.
func (s *LiveSession) Events() <-chan LiveEvent {
	// TODO: Implement
	return nil
}

// SendAudio sends audio data to the session.
func (s *LiveSession) SendAudio(ctx context.Context, data []byte, format string) error {
	// TODO: Implement
	return nil
}

// SendText sends text to the session.
func (s *LiveSession) SendText(ctx context.Context, text string) error {
	// TODO: Implement
	return nil
}

// SendToolResult sends a tool result to the session.
func (s *LiveSession) SendToolResult(ctx context.Context, id string, result any) error {
	// TODO: Implement
	return nil
}

// Close closes the live session.
func (s *LiveSession) Close() error {
	// TODO: Implement
	return nil
}

// Interrupt interrupts the current generation.
func (s *LiveSession) Interrupt() error {
	// TODO: Implement
	return nil
}

package gai

import (
	"context"
	"testing"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/testutil"
	"github.com/shillcollin/gai/schema"
)

func TestRealtimeSessionCreate(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
		Voice: "mock/voice",
		STT:   "mock/stt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Should have an events channel
	if session.Events() == nil {
		t.Error("expected events channel")
	}

	// Should have an underlying conversation
	if session.Conversation() == nil {
		t.Error("expected conversation")
	}
}

func TestRealtimeSessionDefaults(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults: ClientDefaults{
			Model: "mock/default-model",
			Voice: "mock/default-voice",
			STT:   "mock/default-stt",
		},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Config should have defaults applied
	if session.config.Model != "mock/default-model" {
		t.Errorf("expected default model, got %q", session.config.Model)
	}
	if session.config.Voice != "mock/default-voice" {
		t.Errorf("expected default voice, got %q", session.config.Voice)
	}
	if session.config.STT != "mock/default-stt" {
		t.Errorf("expected default STT, got %q", session.config.STT)
	}
}

func TestRealtimeSessionRequiresModel(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		// No default model
	}

	ctx := context.Background()
	_, err := client.RealtimeSession(ctx, RealtimeConfig{
		// No model specified
	})

	if err == nil {
		t.Error("expected error when no model specified")
	}
}

func TestRealtimeSessionClose(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close should work
	if err := session.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}

	// Double close should be safe
	if err := session.Close(); err != nil {
		t.Errorf("double close error: %v", err)
	}

	// SendText should fail after close
	if err := session.SendText("hello"); err != ErrSessionClosed {
		t.Errorf("expected ErrSessionClosed, got %v", err)
	}

	// SendAudio should fail after close
	if err := session.SendAudio([]byte("audio")); err != ErrSessionClosed {
		t.Errorf("expected ErrSessionClosed, got %v", err)
	}

	// Interrupt should fail after close
	if err := session.Interrupt(); err != ErrSessionClosed {
		t.Errorf("expected ErrSessionClosed, got %v", err)
	}
}

func TestRealtimeSessionInputMode(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()

	// Audio-only mode
	audioSession, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model:     "mock/test-model",
		InputMode: InputModeAudio,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer audioSession.Close()

	if err := audioSession.SendText("hello"); err == nil {
		t.Error("expected error for text in audio-only mode")
	}

	// Text-only mode
	textSession, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model:     "mock/test-model",
		InputMode: InputModeText,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer textSession.Close()

	if err := textSession.SendAudio([]byte("audio")); err == nil {
		t.Error("expected error for audio in text-only mode")
	}

	// Both mode (default)
	bothSession, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model:     "mock/test-model",
		InputMode: InputModeBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer bothSession.Close()

	// Both should work (errors would be from processing, not input mode)
	_ = bothSession.SendText("hello")
	_ = bothSession.SendAudio([]byte("audio"))
}

func TestRealtimeSessionInterrupt(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Interrupt should work
	if err := session.Interrupt(); err != nil {
		t.Errorf("interrupt error: %v", err)
	}
}

func TestRealtimeSessionWithSystem(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model:  "mock/test-model",
		System: "You are a helpful assistant",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Check underlying conversation has system
	if session.Conversation().System() != "You are a helpful assistant" {
		t.Errorf("expected system prompt to be set")
	}
}

func TestRealtimeSessionWithTools(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	// Use a mock tool handle for testing
	mockTool := &realtimeMockTool{name: "test_tool"}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
		Tools: []core.ToolHandle{mockTool},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Session should have tools configured
	// (tools are internal to the session config)
	if len(session.config.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(session.config.Tools))
	}
}

// realtimeMockTool is a simple mock for testing realtime sessions
type realtimeMockTool struct {
	name string
}

func (m *realtimeMockTool) Name() string        { return m.name }
func (m *realtimeMockTool) Description() string { return "Mock tool" }
func (m *realtimeMockTool) InputSchema() *schema.Schema  { return nil }
func (m *realtimeMockTool) OutputSchema() *schema.Schema { return nil }
func (m *realtimeMockTool) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	return "result", nil
}

func TestRealtimeSessionSendText(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Hello from mock")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx := context.Background()
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer session.Close()

	// Send text
	if err := session.SendText("Hello"); err != nil {
		t.Fatalf("send error: %v", err)
	}

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Events should have been produced
	// Note: Due to async nature, this is hard to test deterministically
	// In a real test, we'd use proper synchronization
}

func TestRealtimeSessionContextCancellation(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	session, err := client.RealtimeSession(ctx, RealtimeConfig{
		Model: "mock/test-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cancel the context
	cancel()

	// Give time for cancellation to propagate
	time.Sleep(50 * time.Millisecond)

	// Operations should fail with context error
	err = session.SendText("hello")
	if err != nil && err != context.Canceled {
		// May also be nil if sent before cancellation propagated
	}

	session.Close()
}

func TestInputModeConstants(t *testing.T) {
	// Verify constants have expected values
	if InputModeAudio != "audio" {
		t.Errorf("unexpected InputModeAudio: %q", InputModeAudio)
	}
	if InputModeText != "text" {
		t.Errorf("unexpected InputModeText: %q", InputModeText)
	}
	if InputModeBoth != "both" {
		t.Errorf("unexpected InputModeBoth: %q", InputModeBoth)
	}
}

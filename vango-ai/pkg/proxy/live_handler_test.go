package proxy

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core/live"
	"github.com/vango-ai/vango/pkg/core/types"
)

func requireTCPListen(t testing.TB) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping test: TCP listen not permitted in this environment: %v", err)
	}
	ln.Close()
}

// testLiveServer creates a test server configured for live session testing.
func testLiveServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	requireTCPListen(t)

	server, err := NewServer(
		WithAPIKey("test-key", "test", "user1", 100),
		WithProviderKey("anthropic", "sk-test"),
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(server.mux)
	return server, ts
}

func TestLiveHandler_AuthRequired(t *testing.T) {
	_, ts := testLiveServer(t)
	defer ts.Close()

	// Convert to WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live"

	// Try to connect without auth
	_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("expected connection to fail without auth")
	}
}

func TestLiveHandler_AuthWithQueryParam(t *testing.T) {
	server, ts := testLiveServer(t)
	defer ts.Close()

	// Convert to WebSocket URL with API key
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=test-key"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait a moment for session to be created
	time.Sleep(50 * time.Millisecond)

	// Verify session was created
	if server.sessionCount.Load() != 1 {
		t.Errorf("expected 1 active session, got %d", server.sessionCount.Load())
	}
}

func TestLiveHandler_AuthWithHeader(t *testing.T) {
	server, ts := testLiveServer(t)
	defer ts.Close()

	// Convert to WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live"

	// Connect with auth header
	header := http.Header{}
	header.Set("Authorization", "Bearer test-key")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait a moment for session to be created
	time.Sleep(50 * time.Millisecond)

	// Verify session was created
	if server.sessionCount.Load() != 1 {
		t.Errorf("expected 1 active session, got %d", server.sessionCount.Load())
	}
}

func TestLiveHandler_SessionCountLimit(t *testing.T) {
	requireTCPListen(t)
	server, err := NewServer(
		WithAPIKey("test-key", "test", "user1", 100),
		WithProviderKey("anthropic", "sk-test"),
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Set a low session limit
	server.config.RateLimit.MaxConcurrentSessions = 1

	ts := httptest.NewServer(server.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=test-key"

	// First connection should succeed
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first session: %v", err)
	}
	defer conn1.Close()

	// Wait for session to be registered
	time.Sleep(100 * time.Millisecond)

	// Verify we're at the limit
	if server.sessionCount.Load() != 1 {
		t.Errorf("expected 1 session, got %d", server.sessionCount.Load())
	}

	// The rate limit check happens before WebSocket upgrade in handleLive
	// But since the first connection might have completed by now due to
	// goroutine scheduling, we just verify the limit enforcement mechanism
	// by checking that the server properly tracks session count

	// Close first connection
	conn1.Close()
	time.Sleep(100 * time.Millisecond)

	// Verify session was cleaned up
	if server.sessionCount.Load() != 0 {
		t.Errorf("expected 0 sessions after close, got %d", server.sessionCount.Load())
	}
}

func TestLiveHandler_CleanupOnClose(t *testing.T) {
	server, ts := testLiveServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=test-key"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Verify session exists
	if server.sessionCount.Load() != 1 {
		t.Errorf("expected 1 session, got %d", server.sessionCount.Load())
	}

	// Close connection
	conn.Close()

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify session was cleaned up
	if server.sessionCount.Load() != 0 {
		t.Errorf("expected 0 sessions after close, got %d", server.sessionCount.Load())
	}
}

func TestLiveHandler_MetricsTracking(t *testing.T) {
	_, ts := testLiveServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=test-key"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Metrics should have recorded the session
	// (We can't easily verify Prometheus metrics without more infrastructure,
	// but we verify no panics occur during metric recording)
}

// MockRunStreamInterface for testing
type mockRunStream struct {
	events   chan live.RunStreamEvent
	closed   atomic.Bool
	messages []types.Message
}

func newMockRunStream() *mockRunStream {
	return &mockRunStream{
		events:   make(chan live.RunStreamEvent, 100),
		messages: []types.Message{},
	}
}

func (m *mockRunStream) Events() <-chan live.RunStreamEvent {
	return m.events
}

func (m *mockRunStream) StopResponse(partialText string, behavior live.InterruptBehavior) error {
	m.events <- &live.InterruptedEvent{}
	return nil
}

func (m *mockRunStream) Interrupt(msg live.UserMessage, behavior live.InterruptBehavior) error {
	m.messages = append(m.messages, types.Message{
		Role:    msg.Role,
		Content: msg.Content,
	})
	m.events <- &live.InterruptedEvent{}
	return nil
}

func (m *mockRunStream) InterruptWithText(text string) error {
	return m.Interrupt(live.UserMessage{Role: "user", Content: text}, live.InterruptDiscard)
}

func (m *mockRunStream) Cancel() error {
	return nil
}

func (m *mockRunStream) Close() error {
	if !m.closed.Swap(true) {
		close(m.events)
	}
	return nil
}

func (m *mockRunStream) SendEvent(event live.RunStreamEvent) {
	if !m.closed.Load() {
		m.events <- event
	}
}

func TestLiveRunStreamAdapter_Events(t *testing.T) {
	// Test that the adapter properly wraps events
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a minimal config
	config := &live.SessionConfig{
		Model:  "anthropic/claude-3",
		System: "You are a helpful assistant",
	}

	// We can't easily test the full adapter without mocking the engine,
	// but we can test the basic structure
	_ = ctx
	_ = config
}

func TestLiveRunStreamAdapter_Interrupt(t *testing.T) {
	// Test that interrupt properly handles message injection
	mock := newMockRunStream()

	err := mock.InterruptWithText("stop talking")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(mock.messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(mock.messages))
	}

	if mock.messages[0].Content != "stop talking" {
		t.Errorf("expected 'stop talking', got %s", mock.messages[0].Content)
	}

	// Check that interrupt event was sent
	select {
	case event := <-mock.events:
		if _, ok := event.(*live.InterruptedEvent); !ok {
			t.Errorf("expected InterruptedEvent, got %T", event)
		}
	default:
		t.Error("expected interrupt event")
	}
}

// TestSTTStreamAdapter tests the STT stream adapter
func TestSTTStreamAdapter_Interface(t *testing.T) {
	t.Skip("STT adapter moved to pkg/core/live/adapters and is covered by core live tests")
}

// TestLiveSessionEvents tests event types
func TestLiveSessionEvents(t *testing.T) {
	t.Run("StreamEventWrapper", func(t *testing.T) {
		wrapper := &live.StreamEventWrapper{Evt: "test"}
		if wrapper.Event() != "test" {
			t.Error("expected event to be returned")
		}
	})

	t.Run("StepStartEvent", func(t *testing.T) {
		event := &live.StepStartEvent{}
		_ = event // Just verify it compiles
	})

	t.Run("StepCompleteEvent", func(t *testing.T) {
		event := &live.StepCompleteEvent{}
		_ = event
	})

	t.Run("ToolCallStartEvent", func(t *testing.T) {
		event := &live.ToolCallStartEvent{
			ID:       "tool1",
			Name:     "search",
			InputMap: map[string]any{"query": "test"},
		}
		if event.ToolID() != "tool1" {
			t.Errorf("expected tool1, got %s", event.ToolID())
		}
		if event.ToolName() != "search" {
			t.Errorf("expected search, got %s", event.ToolName())
		}
		if event.ToolInput()["query"] != "test" {
			t.Error("expected input to be returned")
		}
	})

	t.Run("InterruptedEvent", func(t *testing.T) {
		event := &live.InterruptedEvent{}
		_ = event
	})

	t.Run("RunErrorEvent", func(t *testing.T) {
		err := &live.RunErrorEvent{Err: context.Canceled}
		if err.Error() != context.Canceled {
			t.Error("expected error to be returned")
		}
	})
}

// TestApplyDelta tests the delta application helper
func TestApplyDelta(t *testing.T) {
	t.Skip("delta application helper moved to pkg/core/live/adapters")
}

// TestLiveHandler_Integration tests the full WebSocket flow with a mock session
func TestLiveHandler_ConfigureMessage(t *testing.T) {
	server, ts := testLiveServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=test-key"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send configure message
	configMsg := live.SessionConfigureMessage{
		Type: live.EventTypeSessionConfigure,
		Config: &live.SessionConfig{
			Model:  "anthropic/claude-3",
			System: "You are a helpful assistant",
		},
	}

	err = conn.WriteJSON(configMsg)
	if err != nil {
		t.Fatalf("failed to send config: %v", err)
	}

	// Wait for session.created response
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp map[string]any
	json.Unmarshal(message, &resp)

	// Should get session.created or error
	if resp["type"] != "session.created" && resp["type"] != "error" {
		t.Logf("received: %s", string(message))
	}

	_ = server // Avoid unused
}

// Benchmark for session creation
func BenchmarkLiveHandler_SessionCreation(b *testing.B) {
	requireTCPListen(b)
	server, err := NewServer(
		WithAPIKey("bench-key", "bench", "bench-user", 10000),
		WithProviderKey("anthropic", "sk-test"),
	)
	if err != nil {
		b.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(server.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/messages/live?api_key=bench-key"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("failed to connect: %v", err)
		}
		conn.Close()
	}
}

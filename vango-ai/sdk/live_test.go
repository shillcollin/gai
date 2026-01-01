package vango

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core/live"
)

// mockLiveServer creates a test WebSocket server for live sessions.
type mockLiveServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	conns     []*websocket.Conn
	connsMu   sync.Mutex
	messages  []interface{}
	msgMu     sync.Mutex
	onMessage func(*websocket.Conn, []byte)
	onBinary  func(*websocket.Conn, []byte)
}

func newMockLiveServer() *mockLiveServer {
	m := &mockLiveServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		m.connsMu.Lock()
		m.conns = append(m.conns, conn)
		m.connsMu.Unlock()

		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			m.msgMu.Lock()
			m.messages = append(m.messages, data)
			m.msgMu.Unlock()

			if messageType == websocket.BinaryMessage {
				if m.onBinary != nil {
					m.onBinary(conn, data)
				}
			} else {
				if m.onMessage != nil {
					m.onMessage(conn, data)
				}
			}
		}
	}))

	return m
}

func (m *mockLiveServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockLiveServer) Close() {
	m.connsMu.Lock()
	for _, conn := range m.conns {
		conn.Close()
	}
	m.connsMu.Unlock()
	m.server.Close()
}

func (m *mockLiveServer) sendJSON(conn *websocket.Conn, v interface{}) error {
	return conn.WriteJSON(v)
}

func (m *mockLiveServer) sendBinary(conn *websocket.Conn, data []byte) error {
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// TestLiveStreamConnect tests basic WebSocket connection.
func TestLiveStreamConnect(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	sessionCreated := make(chan bool, 1)
	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}

		if msg.Type == live.EventTypeSessionConfigure {
			// Send session.created response
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session-123",
				Config: live.SessionInfoConfig{
					Model:      "claude-3-opus",
					SampleRate: 24000,
					Channels:   1,
				},
			})
			sessionCreated <- true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &LiveConfig{
		Model: "claude-3-opus",
	}

	stream, err := newLiveStream(ctx, server.URL(), cfg)
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	select {
	case <-sessionCreated:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for session.configure message")
	}

	// Wait for session created event
	select {
	case event := <-stream.Events():
		if createdEvent, ok := event.(LiveSessionCreatedEvent); ok {
			if createdEvent.SessionID != "test-session-123" {
				t.Errorf("Expected session ID 'test-session-123', got '%s'", createdEvent.SessionID)
			}
			if createdEvent.Model != "claude-3-opus" {
				t.Errorf("Expected model 'claude-3-opus', got '%s'", createdEvent.Model)
			}
		} else {
			t.Fatalf("Expected LiveSessionCreatedEvent, got %T", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for session created event")
	}
}

// TestLiveStreamSendText tests sending text input.
func TestLiveStreamSendText(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	textReceived := make(chan string, 1)
	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		json.Unmarshal(data, &msg)

		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
				Config:    live.SessionInfoConfig{Model: "claude-3-opus"},
			})
		} else if msg.Type == live.EventTypeInputText {
			textReceived <- msg.Text
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"})
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	// Wait for ready
	time.Sleep(100 * time.Millisecond)

	if err := stream.SendText("Hello, world!"); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	select {
	case text := <-textReceived:
		if text != "Hello, world!" {
			t.Errorf("Expected 'Hello, world!', got '%s'", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for text message")
	}
}

// TestLiveStreamSendAudio tests sending audio data.
func TestLiveStreamSendAudio(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	audioReceived := make(chan []byte, 1)
	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &msg)
		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
			})
		}
	}
	server.onBinary = func(conn *websocket.Conn, data []byte) {
		audioReceived <- data
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"})
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	time.Sleep(100 * time.Millisecond)

	testAudio := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := stream.SendAudio(testAudio); err != nil {
		t.Fatalf("Failed to send audio: %v", err)
	}

	select {
	case received := <-audioReceived:
		if !bytes.Equal(received, testAudio) {
			t.Errorf("Audio data mismatch: expected %v, got %v", testAudio, received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for audio data")
	}
}

// TestLiveStreamReceiveAudio tests receiving audio output.
func TestLiveStreamReceiveAudio(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &msg)
		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
			})

			// Send audio data after a short delay
			go func() {
				time.Sleep(100 * time.Millisecond)
				server.sendBinary(conn, []byte{0xAA, 0xBB, 0xCC})
			}()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"})
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	select {
	case audioData := <-stream.Audio():
		if !bytes.Equal(audioData, []byte{0xAA, 0xBB, 0xCC}) {
			t.Errorf("Audio data mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for audio")
	}
}

// TestLiveStreamEvents tests event handling.
func TestLiveStreamEvents(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &msg)

		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
			})

			// Send various events
			go func() {
				time.Sleep(50 * time.Millisecond)

				// VAD listening
				server.sendJSON(conn, map[string]string{"type": live.EventTypeVADListening})

				// Transcript delta
				server.sendJSON(conn, live.TranscriptDeltaMessage{
					Type:  live.EventTypeTranscriptDelta,
					Delta: "hello",
				})

				// Input committed
				server.sendJSON(conn, live.InputCommittedMessage{
					Type:       live.EventTypeInputCommitted,
					Transcript: "hello world",
				})

				// Content block delta
				server.sendJSON(conn, map[string]interface{}{
					"type":  live.EventTypeContentBlockDelta,
					"index": 0,
					"delta": map[string]string{"type": "text_delta", "text": "Response"},
				})

				// Message stop
				server.sendJSON(conn, live.MessageStopMessage{
					Type:       live.EventTypeMessageStop,
					StopReason: "end_turn",
				})
			}()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"})
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	var events []LiveEvent
	timeout := time.After(2 * time.Second)

eventLoop:
	for {
		select {
		case event, ok := <-stream.Events():
			if !ok {
				break eventLoop
			}
			events = append(events, event)
			if _, ok := event.(LiveMessageStopEvent); ok {
				break eventLoop
			}
		case <-timeout:
			break eventLoop
		}
	}

	// Verify we received expected events
	eventTypes := make([]string, len(events))
	for i, e := range events {
		eventTypes[i] = e.liveEventType()
	}

	if len(events) < 5 {
		t.Errorf("Expected at least 5 events, got %d: %v", len(events), eventTypes)
	}

	// Check for expected event types
	hasSessionCreated := false
	hasVAD := false
	hasMessageStop := false
	for _, e := range events {
		switch e.(type) {
		case LiveSessionCreatedEvent:
			hasSessionCreated = true
		case LiveVADEvent:
			hasVAD = true
		case LiveMessageStopEvent:
			hasMessageStop = true
		}
	}

	if !hasSessionCreated {
		t.Error("Missing session.created event")
	}
	if !hasVAD {
		t.Error("Missing VAD event")
	}
	if !hasMessageStop {
		t.Error("Missing message.stop event")
	}
}

// TestLiveStreamToolHandler tests automatic tool execution.
func TestLiveStreamToolHandler(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	toolResultReceived := make(chan map[string]interface{}, 1)
	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg map[string]interface{}
		json.Unmarshal(data, &msg)

		msgType, _ := msg["type"].(string)

		if msgType == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
			})

			// Send a tool use event
			go func() {
				time.Sleep(100 * time.Millisecond)
				server.sendJSON(conn, map[string]interface{}{
					"type":  live.EventTypeToolUse,
					"id":    "tool-123",
					"name":  "test_tool",
					"input": map[string]interface{}{"arg": "value"},
				})
			}()
		} else if msgType == live.EventTypeToolResult {
			toolResultReceived <- msg
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"},
		WithLiveToolHandler("test_tool", func(ctx context.Context, input json.RawMessage) (any, error) {
			return "tool executed successfully", nil
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	select {
	case result := <-toolResultReceived:
		if result["tool_use_id"] != "tool-123" {
			t.Errorf("Expected tool_use_id 'tool-123', got '%v'", result["tool_use_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for tool result")
	}
}

// TestLiveStreamInterrupt tests interrupt functionality.
func TestLiveStreamInterrupt(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	interruptReceived := make(chan string, 1)
	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type       string `json:"type"`
			Transcript string `json:"transcript"`
		}
		json.Unmarshal(data, &msg)

		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "test-session",
			})
		} else if msg.Type == live.EventTypeInputInterrupt {
			interruptReceived <- msg.Transcript
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"})
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}
	defer stream.Close()

	time.Sleep(100 * time.Millisecond)

	if err := stream.Interrupt("stop talking"); err != nil {
		t.Fatalf("Failed to send interrupt: %v", err)
	}

	select {
	case transcript := <-interruptReceived:
		if transcript != "stop talking" {
			t.Errorf("Expected 'stop talking', got '%s'", transcript)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for interrupt")
	}
}

// TestLiveStreamCallbacks tests connection callbacks.
func TestLiveStreamCallbacks(t *testing.T) {
	server := newMockLiveServer()
	defer server.Close()

	server.onMessage = func(conn *websocket.Conn, data []byte) {
		var msg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(data, &msg)
		if msg.Type == live.EventTypeSessionConfigure {
			server.sendJSON(conn, live.SessionCreatedMessage{
				Type:      live.EventTypeSessionCreated,
				SessionID: "callback-test-session",
			})
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connected := make(chan string, 1)
	disconnected := make(chan bool, 1)

	stream, err := newLiveStream(ctx, server.URL(), &LiveConfig{Model: "claude-3-opus"},
		WithLiveOnConnect(func(sessionID string) {
			connected <- sessionID
		}),
		WithLiveOnDisconnect(func() {
			disconnected <- true
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create LiveStream: %v", err)
	}

	select {
	case sessionID := <-connected:
		if sessionID != "callback-test-session" {
			t.Errorf("Expected session ID 'callback-test-session', got '%s'", sessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for connect callback")
	}

	stream.Close()

	select {
	case <-disconnected:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for disconnect callback")
	}
}

// TestLiveConfigBuilder tests configuration builder methods.
func TestLiveConfigBuilder(t *testing.T) {
	cfg := NewLiveConfig("claude-3-opus").
		WithSystem("You are a helpful assistant.").
		WithVoice("alloy").
		WithInterruptMode(LiveInterruptModeManual).
		WithVADThreshold(0.03).
		WithVADSilenceDuration(800)

	if cfg.Model != "claude-3-opus" {
		t.Errorf("Expected model 'claude-3-opus', got '%s'", cfg.Model)
	}
	if cfg.System != "You are a helpful assistant." {
		t.Errorf("Expected system prompt, got '%s'", cfg.System)
	}
	if cfg.Voice == nil || cfg.Voice.Output == nil || cfg.Voice.Output.Voice != "alloy" {
		t.Error("Expected voice 'alloy'")
	}
	if cfg.Voice.Interrupt == nil || cfg.Voice.Interrupt.Mode != LiveInterruptModeManual {
		t.Error("Expected interrupt mode 'manual'")
	}
	if cfg.Voice.VAD == nil || cfg.Voice.VAD.EnergyThreshold != 0.03 {
		t.Error("Expected VAD threshold 0.03")
	}
	if cfg.Voice.VAD.SilenceDurationMs != 800 {
		t.Errorf("Expected silence duration 800, got %d", cfg.Voice.VAD.SilenceDurationMs)
	}
}

// TestPCMToWAVWithFormat tests audio format conversion.
func TestPCMToWAVWithFormat(t *testing.T) {
	pcmData := make([]byte, 4800) // 100ms of 24kHz mono 16-bit audio
	for i := range pcmData {
		pcmData[i] = byte(i % 256)
	}

	wavData := PCMToWAVWithFormat(pcmData, DefaultLiveAudioFormat())

	// Check WAV header
	if string(wavData[:4]) != "RIFF" {
		t.Error("Missing RIFF header")
	}
	if string(wavData[8:12]) != "WAVE" {
		t.Error("Missing WAVE format")
	}
	if string(wavData[12:16]) != "fmt " {
		t.Error("Missing fmt subchunk")
	}

	// Check data chunk
	expectedSize := 44 + len(pcmData) // 44-byte header + data
	if len(wavData) != expectedSize {
		t.Errorf("Expected WAV size %d, got %d", expectedSize, len(wavData))
	}
}

// TestSplitAudioIntoChunks tests audio chunking.
func TestSplitAudioIntoChunks(t *testing.T) {
	format := DefaultLiveAudioFormat()
	// 24000 samples/sec * 2 bytes/sample * 1 channel = 48000 bytes/sec
	// 100ms = 4800 bytes
	audioData := make([]byte, 48000) // 1 second of audio

	chunks := SplitAudioIntoChunks(audioData, format, 100)

	expectedChunks := 10 // 1000ms / 100ms
	if len(chunks) != expectedChunks {
		t.Errorf("Expected %d chunks, got %d", expectedChunks, len(chunks))
	}

	expectedChunkSize := 4800
	for i, chunk := range chunks {
		if len(chunk) != expectedChunkSize {
			t.Errorf("Chunk %d: expected size %d, got %d", i, expectedChunkSize, len(chunk))
		}
	}
}

// TestEventTypeHelpers tests event type helper functions.
func TestEventTypeHelpers(t *testing.T) {
	tests := []struct {
		event    LiveEvent
		isAudio  bool
		isText   bool
		isTool   bool
		isError  bool
		isInterrupt bool
	}{
		{LiveAudioEvent{Data: []byte{1, 2, 3}}, true, false, false, false, false},
		{LiveTextDeltaEvent{Text: "hello"}, false, true, false, false, false},
		{LiveToolCallEvent{ID: "1", Name: "test"}, false, false, true, false, false},
		{LiveErrorEvent{Code: "err", Message: "msg"}, false, false, false, true, false},
		{LiveInterruptEvent{State: "detecting"}, false, false, false, false, true},
	}

	for _, tt := range tests {
		if IsAudioEvent(tt.event) != tt.isAudio {
			t.Errorf("IsAudioEvent(%T) = %v, want %v", tt.event, IsAudioEvent(tt.event), tt.isAudio)
		}
		if IsTextEvent(tt.event) != tt.isText {
			t.Errorf("IsTextEvent(%T) = %v, want %v", tt.event, IsTextEvent(tt.event), tt.isText)
		}
		if IsToolCallEvent(tt.event) != tt.isTool {
			t.Errorf("IsToolCallEvent(%T) = %v, want %v", tt.event, IsToolCallEvent(tt.event), tt.isTool)
		}
		if IsErrorEvent(tt.event) != tt.isError {
			t.Errorf("IsErrorEvent(%T) = %v, want %v", tt.event, IsErrorEvent(tt.event), tt.isError)
		}
		if IsInterruptEvent(tt.event) != tt.isInterrupt {
			t.Errorf("IsInterruptEvent(%T) = %v, want %v", tt.event, IsInterruptEvent(tt.event), tt.isInterrupt)
		}
	}
}

// TestEventTypeAssertions tests event type assertion helpers.
func TestEventTypeAssertions(t *testing.T) {
	audioEvent := LiveAudioEvent{Data: []byte{1, 2, 3}, Format: "pcm"}

	if e, ok := AsAudioEvent(audioEvent); !ok {
		t.Error("AsAudioEvent should return true for LiveAudioEvent")
	} else if e.Format != "pcm" {
		t.Error("AsAudioEvent should preserve data")
	}

	if _, ok := AsTextDeltaEvent(audioEvent); ok {
		t.Error("AsTextDeltaEvent should return false for LiveAudioEvent")
	}

	textEvent := LiveTextDeltaEvent{Text: "hello"}
	if e, ok := AsTextDeltaEvent(textEvent); !ok {
		t.Error("AsTextDeltaEvent should return true for LiveTextDeltaEvent")
	} else if e.Text != "hello" {
		t.Error("AsTextDeltaEvent should preserve data")
	}
}

// TestBuildLiveURL tests WebSocket URL construction.
func TestBuildLiveURL(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		apiKey    string
		wantURL   string
		wantError bool
	}{
		{
			name:    "http to ws",
			baseURL: "http://localhost:8080",
			apiKey:  "",
			wantURL: "ws://localhost:8080/v1/messages/live",
		},
		{
			name:    "https to wss",
			baseURL: "https://api.example.com",
			apiKey:  "",
			wantURL: "wss://api.example.com/v1/messages/live",
		},
		{
			name:    "with api key",
			baseURL: "http://localhost:8080",
			apiKey:  "test-key",
			wantURL: "ws://localhost:8080/v1/messages/live?api_key=test-key",
		},
		{
			name:    "with trailing slash",
			baseURL: "http://localhost:8080/",
			apiKey:  "",
			wantURL: "ws://localhost:8080/v1/messages/live",
		},
		{
			name:      "empty base URL",
			baseURL:   "",
			apiKey:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				baseURL: tt.baseURL,
				apiKey:  tt.apiKey,
			}

			url, err := client.buildLiveURL()

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if url != tt.wantURL {
				t.Errorf("Expected URL '%s', got '%s'", tt.wantURL, url)
			}
		})
	}
}

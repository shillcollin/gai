package live

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Mock implementations ---

type mockSTTStream struct {
	mu         sync.Mutex
	transcript strings.Builder
	delta      strings.Builder
	closed     bool
}

func newMockSTTStream() *mockSTTStream {
	return &mockSTTStream{}
}

func (m *mockSTTStream) Write(audio []byte) error {
	return nil
}

func (m *mockSTTStream) Transcript() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.transcript.String()
}

func (m *mockSTTStream) TranscriptDelta() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	delta := m.delta.String()
	m.delta.Reset()
	return delta
}

func (m *mockSTTStream) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transcript.Reset()
	m.delta.Reset()
}

func (m *mockSTTStream) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockSTTStream) AddTranscript(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transcript.WriteString(text)
	m.delta.WriteString(text)
}

// mockRunStreamEvent implements RunStreamEvent
type mockRunStreamEvent struct {
	eventType string
	text      string
}

func (e mockRunStreamEvent) runStreamEventType() string { return e.eventType }

// mockStreamEventWrapper wraps a stream event
type mockStreamEventWrapper struct {
	event any
}

func (e mockStreamEventWrapper) runStreamEventType() string { return "stream_event" }
func (e mockStreamEventWrapper) Event() any                 { return e.event }

// mockTextDelta implements text delta extraction
type mockTextDelta struct {
	text string
}

func (d mockTextDelta) TextDelta() (string, bool) { return d.text, true }

// mockRunStream implements RunStreamInterface
type mockRunStream struct {
	events       chan RunStreamEvent
	closed       atomic.Bool
	interrupted  atomic.Bool
	stopped      atomic.Bool
	interruptMsg string
	mu           sync.Mutex
}

func newMockRunStream() *mockRunStream {
	return &mockRunStream{
		events: make(chan RunStreamEvent, 100),
	}
}

func (m *mockRunStream) Events() <-chan RunStreamEvent {
	return m.events
}

func (m *mockRunStream) Interrupt(msg UserMessage, behavior InterruptBehavior) error {
	m.mu.Lock()
	m.interruptMsg = msg.Content
	m.mu.Unlock()
	m.interrupted.Store(true)
	return nil
}

func (m *mockRunStream) StopResponse(partialText string, behavior InterruptBehavior) error {
	m.stopped.Store(true)
	return nil
}

func (m *mockRunStream) InterruptWithText(text string) error {
	m.mu.Lock()
	m.interruptMsg = text
	m.mu.Unlock()
	m.interrupted.Store(true)
	return nil
}

func (m *mockRunStream) Cancel() error {
	return nil
}

func (m *mockRunStream) Close() error {
	if m.closed.CompareAndSwap(false, true) {
		close(m.events)
	}
	return nil
}

func (m *mockRunStream) SendEvent(event RunStreamEvent) {
	if !m.closed.Load() {
		m.events <- event
	}
}

func (m *mockRunStream) SendTextDelta(text string) {
	m.SendEvent(mockStreamEventWrapper{event: mockTextDelta{text: text}})
}

// --- Test helpers ---

// --- Tests ---

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state    SessionState
		expected string
	}{
		{StateConfiguring, "configuring"},
		{StateListening, "listening"},
		{StateProcessing, "processing"},
		{StateSpeaking, "speaking"},
		{StateInterruptCheck, "interrupt_check"},
		{StateClosed, "closed"},
		{SessionState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestNewLiveSession(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})

	if session == nil {
		t.Fatal("expected non-nil session")
	}

	if session.ID() == "" {
		t.Error("expected non-empty session ID")
	}

	if session.State() != StateConfiguring {
		t.Errorf("expected initial state Configuring, got %v", session.State())
	}

	session.Close()
}

func TestNewLiveSession_CustomID(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		SessionID:  "custom-session-123",
	})
	defer session.Close()

	if session.ID() != "custom-session-123" {
		t.Errorf("expected custom session ID, got %s", session.ID())
	}
}

func TestLiveSession_Configure(t *testing.T) {
	mockLLM := MockLLMFunc(nil, "YES", 0)

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		LLMFunc:    mockLLM,
		STTFactory: func(ctx context.Context, config *VoiceInputConfig) (STTStream, error) {
			return newMockSTTStream(), nil
		},
	})
	defer session.Close()

	config := &SessionConfig{
		Model:  "test-model",
		System: "You are a helpful assistant.",
	}

	err := session.Configure(config)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if session.State() != StateListening {
		t.Errorf("expected state Listening after configure, got %v", session.State())
	}
}

func TestLiveSession_Configure_AppliesDefaults(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	config := &SessionConfig{
		Model: "test-model",
	}

	err := session.Configure(config)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if session.vad == nil {
		t.Error("expected VAD to be initialized")
	}
	if session.tts == nil {
		t.Error("expected TTS pipeline to be initialized")
	}
	if session.audioBuffer == nil {
		t.Error("expected audio buffer to be initialized")
	}
}

func TestLiveSession_Configure_InvalidState(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.setState(StateSpeaking)

	err := session.Configure(&SessionConfig{Model: "test2"})
	if err == nil {
		t.Error("expected error when configuring in Speaking state")
	}
}

func TestLiveSession_UpdateConfig(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "model-1"})

	err := session.UpdateConfig(&SessionConfig{
		Model:  "model-2",
		System: "Updated system prompt",
	})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	session.configMu.RLock()
	model := session.config.Model
	system := session.config.System
	session.configMu.RUnlock()

	if model != "model-2" {
		t.Errorf("expected model 'model-2', got %q", model)
	}
	if system != "Updated system prompt" {
		t.Errorf("expected updated system prompt, got %q", system)
	}
}

func TestLiveSession_UpdateConfig_NotConfigured(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	err := session.UpdateConfig(&SessionConfig{Model: "test"})
	if err == nil {
		t.Error("expected error when updating unconfigured session")
	}
}

func TestLiveSession_Close(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})

	session.Configure(&SessionConfig{Model: "test"})

	err := session.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	if session.State() != StateClosed {
		t.Errorf("expected state Closed, got %v", session.State())
	}

	session.Close() // Second close should not panic
}

func TestLiveSession_Done(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})

	done := session.Done()
	session.Close()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel not closed after Close()")
	}
}

func TestLiveSession_HandleAudioInput_VAD(t *testing.T) {
	mockSTT := newMockSTTStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		STTFactory: func(ctx context.Context, config *VoiceInputConfig) (STTStream, error) {
			return mockSTT, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})

	mockSTT.AddTranscript("hello world")

	audio := make([]byte, 4800)
	for i := 0; i < len(audio); i += 2 {
		audio[i] = 0x00
		audio[i+1] = 0x40
	}

	session.handleAudioInput(audio)

	if session.vad.State() != VADStateSpeech && session.vad.State() != VADStateSilence {
		t.Errorf("expected VAD to transition to Speech or Silence, got %v", session.vad.State())
	}
}

func TestLiveSession_HandleTextInput(t *testing.T) {
	var runStreamCreated atomic.Bool
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			runStreamCreated.Store(true)
			return mockStream, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.Start()

	_ = session.EnqueueText("Hello, world!")

	time.Sleep(100 * time.Millisecond)

	if !runStreamCreated.Load() {
		t.Error("expected RunStream to be created after text input")
	}

	mockStream.Close()
}

func TestLiveSession_GracePeriodCancelsResponse(t *testing.T) {
	mockSTT := newMockSTTStream()
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		STTFactory: func(ctx context.Context, config *VoiceInputConfig) (STTStream, error) {
			return mockSTT, nil
		},
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			return mockStream, nil
		},
	})
	defer session.Close()

	semantic := false
	err := session.Configure(&SessionConfig{
		Model: "test-model",
		Voice: &VoiceConfig{
			VAD: &VADConfig{
				EnergyThreshold: 0.01,
				SemanticCheck:   &semantic,
			},
		},
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	session.Start()

	speech := make([]byte, 4800)
	for i := 0; i < len(speech); i += 2 {
		speech[i] = 0x00
		speech[i+1] = 0x40
	}
	silence := make([]byte, len(speech))

	// Prime lastUserSpeechAt by simulating speech before commit.
	mockSTT.AddTranscript("hello")
	session.handleAudioInput(speech)

	// Commit a turn to activate grace window.
	session.vad.AddTranscript("hello")
	session.commitTurn()
	time.Sleep(25 * time.Millisecond)

	if session.State() != StateProcessing {
		t.Fatalf("expected StateProcessing after commit, got %v", session.State())
	}

	// Ensure we observe a transition to "not speaking".
	session.handleAudioInput(silence)

	// User starts speaking again within grace window -> cancel response and resume listening.
	mockSTT.AddTranscript("again")
	session.handleAudioInput(speech)

	if !mockStream.stopped.Load() {
		t.Error("expected StopResponse to be called on grace cancellation")
	}
	if session.State() != StateListening {
		t.Fatalf("expected StateListening after grace cancellation, got %v", session.State())
	}
	if got := session.vad.GetTranscript(); !strings.Contains(got, "hello") {
		t.Fatalf("expected transcript to preserve committed prefix, got %q", got)
	}
}

func TestLiveSession_ConfirmAudioInterrupt_SeedsVAD(t *testing.T) {
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			return mockStream, nil
		},
	})
	defer session.Close()

	if err := session.Configure(&SessionConfig{Model: "test"}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	session.Start()

	session.runStreamMu.Lock()
	session.runStream = mockStream
	session.runStreamMu.Unlock()

	session.setState(StateSpeaking)

	session.confirmAudioInterrupt("actually wait", "partial response", 123)

	if !mockStream.stopped.Load() {
		t.Fatal("expected StopResponse to be called")
	}
	if mockStream.interrupted.Load() {
		t.Fatal("did not expect Interrupt() to be called (should wait for VAD commit)")
	}
	if session.State() != StateListening {
		t.Fatalf("expected StateListening, got %v", session.State())
	}
	if got := session.vad.GetTranscript(); !strings.Contains(got, "actually wait") {
		t.Fatalf("expected VAD transcript to be seeded, got %q", got)
	}
}

func TestLiveSession_JSONMessageHandling(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	configMsg := map[string]any{
		"type": "session.configure",
		"config": map[string]any{
			"model":  "test-model",
			"system": "Test system",
		},
	}
	data, _ := json.Marshal(configMsg)
	session.handleJSONMessage(data)

	time.Sleep(100 * time.Millisecond)

	if session.State() != StateListening {
		t.Errorf("expected state Listening after configure message, got %v", session.State())
	}
}

func TestLiveSession_SendEvent(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	select {
	case evt := <-session.Events():
		if _, ok := evt.(SessionCreatedEvent); !ok {
			t.Fatalf("expected SessionCreatedEvent, got %T", evt)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for SessionCreatedEvent")
	}

	session.sendEvent(VADListeningEvent{})

	select {
	case evt := <-session.Events():
		if _, ok := evt.(VADListeningEvent); !ok {
			t.Fatalf("expected VADListeningEvent, got %T", evt)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for VADListeningEvent")
	}
}

func TestLiveSession_ForceInterrupt(t *testing.T) {
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			return mockStream, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.Start()

	session.runStreamMu.Lock()
	session.runStream = mockStream
	session.runStreamMu.Unlock()

	session.setState(StateSpeaking)

	session.ForceInterrupt("stop now")

	time.Sleep(50 * time.Millisecond)

	if !mockStream.stopped.Load() {
		t.Error("expected StopResponse to be called on force interrupt")
	}

	if !mockStream.interrupted.Load() {
		t.Error("expected RunStream to be interrupted")
	}

	mockStream.mu.Lock()
	msg := mockStream.interruptMsg
	mockStream.mu.Unlock()

	if msg != "stop now" {
		t.Errorf("expected interrupt message 'stop now', got %q", msg)
	}

	if session.State() == StateClosed {
		t.Errorf("expected session to remain open after force interrupt")
	}
}

func TestLiveSession_CommitTurn(t *testing.T) {
	var capturedMessage string
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			capturedMessage = firstMessage
			return mockStream, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.Start()

	session.vad.AddTranscript("Hello, assistant!")

	session.commitTurn()

	time.Sleep(100 * time.Millisecond)

	if capturedMessage != "Hello, assistant!" {
		t.Errorf("expected first message 'Hello, assistant!', got %q", capturedMessage)
	}

	mockStream.Close()
}

func TestLiveSession_CommitTurn_EmptyTranscript(t *testing.T) {
	var runStreamCreated atomic.Bool

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			runStreamCreated.Store(true)
			return newMockRunStream(), nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})

	session.commitTurn()

	time.Sleep(50 * time.Millisecond)

	if runStreamCreated.Load() {
		t.Error("RunStream should not be created for empty transcript")
	}

	if session.State() != StateListening {
		t.Errorf("expected state Listening after empty commit, got %v", session.State())
	}
}

func TestLiveSession_ProcessRunStreamEvents(t *testing.T) {
	mockStream := newMockRunStream()

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				mockStream.SendTextDelta("Hello")
				time.Sleep(20 * time.Millisecond)
				mockStream.SendTextDelta(" world")
				time.Sleep(20 * time.Millisecond)
				mockStream.Close()
			}()
			return mockStream, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.Start()

	session.vad.AddTranscript("Test")
	session.commitTurn()

	time.Sleep(200 * time.Millisecond)

	// State should be back to listening
	if session.State() != StateListening {
		t.Errorf("expected state Listening after stream ends, got %v", session.State())
	}
}

func TestLiveSession_SubsequentTurns_UseInterrupt(t *testing.T) {
	mockStream := newMockRunStream()
	createCount := 0

	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
		RunStreamCreator: func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error) {
			createCount++
			return mockStream, nil
		},
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})
	session.Start()

	// First turn
	session.vad.AddTranscript("First message")
	session.commitTurn()
	time.Sleep(50 * time.Millisecond)

	if createCount != 1 {
		t.Errorf("expected RunStream to be created once, got %d", createCount)
	}

	// Set RunStream (simulating it's still active)
	session.runStreamMu.Lock()
	session.runStream = mockStream
	session.runStreamMu.Unlock()
	session.setState(StateListening)

	// Second turn - should use Interrupt, not create new stream
	session.vad.AddTranscript("Second message")
	session.commitTurn()
	time.Sleep(50 * time.Millisecond)

	// Stream should NOT be created again
	if createCount != 1 {
		t.Errorf("expected RunStream to NOT be created again, got %d creates", createCount)
	}

	// Should have called Interrupt
	if !mockStream.interrupted.Load() {
		t.Error("expected RunStream.Interrupt to be called for second turn")
	}

	mockStream.Close()
}

func TestLiveSession_ConcurrentAccess(t *testing.T) {
	session := NewLiveSession(LiveSessionConfig{
		Connection: nil,
	})
	defer session.Close()

	session.Configure(&SessionConfig{Model: "test"})

	var wg sync.WaitGroup
	iterations := 50

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = session.State()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			session.configMu.RLock()
			_ = session.config
			session.configMu.RUnlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			session.sendEvent(VADListeningEvent{})
		}
	}()

	wg.Wait()
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	if id1 == "" {
		t.Error("expected non-empty session ID")
	}

	if id1 == id2 {
		t.Error("expected unique session IDs")
	}

	if !strings.HasPrefix(id1, "live_") {
		t.Errorf("expected session ID to start with 'live_', got %q", id1)
	}
}

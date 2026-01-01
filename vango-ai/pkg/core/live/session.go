// Package live provides real-time bidirectional voice session functionality.
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

const (
	userGracePeriod = 5 * time.Second
)

// SessionState represents the current state of the live session.
type SessionState int

const (
	// StateConfiguring is the initial state before configuration is received.
	StateConfiguring SessionState = iota

	// StateListening indicates the session is listening for user input.
	StateListening

	// StateProcessing indicates the session is processing user input (agent turn).
	StateProcessing

	// StateSpeaking indicates the session is generating and sending TTS output.
	StateSpeaking

	// StateInterruptCheck indicates TTS is paused while checking if speech is an interrupt.
	StateInterruptCheck

	// StateClosed indicates the session has been closed.
	StateClosed
)

// String returns a string representation of the session state.
func (s SessionState) String() string {
	switch s {
	case StateConfiguring:
		return "configuring"
	case StateListening:
		return "listening"
	case StateProcessing:
		return "processing"
	case StateSpeaking:
		return "speaking"
	case StateInterruptCheck:
		return "interrupt_check"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// STTStream is the interface for speech-to-text streaming.
type STTStream interface {
	Write(audio []byte) error
	Transcript() string
	TranscriptDelta() string
	Reset()
	Close() error
}

// InterruptBehavior specifies how to handle partial responses when interrupted.
type InterruptBehavior int

const (
	InterruptDiscard     InterruptBehavior = iota // Discard partial response
	InterruptSavePartial                          // Save partial response as-is
	InterruptSaveMarked                           // Save with [interrupted] marker
)

// RunStreamEvent is an event from the RunStream.
type RunStreamEvent interface {
	runStreamEventType() string
}

// --- RunStream Event Types ---

// StreamEventWrapper wraps an underlying streaming event.
type StreamEventWrapper struct {
	Evt any
}

func (e *StreamEventWrapper) runStreamEventType() string { return "stream_event" }

// Event returns the underlying event.
func (e *StreamEventWrapper) Event() any { return e.Evt }

// StepStartEvent signals the start of an agent step.
type StepStartEvent struct{}

func (e *StepStartEvent) runStreamEventType() string { return "step_start" }

// StepCompleteEvent signals the completion of an agent step.
type StepCompleteEvent struct{}

func (e *StepCompleteEvent) runStreamEventType() string { return "step_complete" }

// ToolCallStartEvent signals a tool call is starting.
type ToolCallStartEvent struct {
	ID       string
	Name     string
	InputMap map[string]any
}

func (e *ToolCallStartEvent) runStreamEventType() string { return "tool_call_start" }
func (e *ToolCallStartEvent) ToolID() string             { return e.ID }
func (e *ToolCallStartEvent) ToolName() string           { return e.Name }
func (e *ToolCallStartEvent) ToolInput() map[string]any  { return e.InputMap }

// InterruptedEvent signals that the stream was interrupted.
type InterruptedEvent struct{}

func (e *InterruptedEvent) runStreamEventType() string { return "interrupted" }

// RunCompleteEvent signals that the run is complete.
type RunCompleteEvent struct{}

func (e *RunCompleteEvent) runStreamEventType() string { return "run_complete" }

// RunErrorEvent signals an error occurred during the run.
type RunErrorEvent struct {
	Err error
}

func (e *RunErrorEvent) runStreamEventType() string { return "error" }
func (e *RunErrorEvent) Error() error               { return e.Err }

// TextDeltaAdapter adapts text deltas for the forwardRunStreamEvent handler.
type TextDeltaAdapter struct {
	Text string
}

func (e *TextDeltaAdapter) TextDelta() (string, bool) { return e.Text, true }

// RunStreamInterface abstracts the RunStream for dependency injection.
type RunStreamInterface interface {
	// Events returns the channel of run stream events.
	Events() <-chan RunStreamEvent

	// StopResponse stops the current response without injecting a message.
	// Implementations may optionally persist partial assistant output based on behavior.
	StopResponse(partialText string, behavior InterruptBehavior) error

	// Interrupt stops the current stream, saves partial per behavior,
	// injects a new message, and continues the conversation.
	Interrupt(msg UserMessage, behavior InterruptBehavior) error

	// InterruptWithText is a convenience for text interrupts.
	InterruptWithText(text string) error

	// Cancel stops the stream without injecting a message.
	Cancel() error

	// Close closes the stream completely.
	Close() error
}

// UserMessage represents a user message to inject.
type UserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunStreamCreator creates the initial RunStream for a session.
// It receives the config and the first user message.
type RunStreamCreator func(ctx context.Context, config *SessionConfig, firstMessage string) (RunStreamInterface, error)

// STTStreamFactory creates a new STT stream.
type STTStreamFactory func(ctx context.Context, config *VoiceInputConfig) (STTStream, error)

// TTSFactory creates a new streaming TTS context for a single assistant response.
type TTSFactory func(ctx context.Context, config *VoiceOutputConfig) (*tts.StreamingContext, error)

// LiveSession manages a real-time bidirectional voice session.
type LiveSession struct {
	id   string
	conn *websocket.Conn

	// Configuration
	config   *SessionConfig
	configMu sync.RWMutex

	// Dependency injection
	runStreamCreator RunStreamCreator
	sttFactory       STTStreamFactory
	ttsFactory       TTSFactory
	llmFunc          LLMFunc

	// Active components
	runStream         RunStreamInterface
	runStreamMu       sync.Mutex
	stt               STTStream
	sttMu             sync.Mutex
	vad               *HybridVAD
	tts               *TTSPipeline
	interruptDetector *InterruptDetector
	audioBuffer       *AudioBuffer

	// State
	state   SessionState
	stateMu sync.RWMutex

	// Partial text tracking for interrupts
	partialText   strings.Builder
	partialTextMu sync.Mutex

	// User grace period handling (Phase 7 live mode)
	graceMu                  sync.Mutex
	graceActive              bool
	graceDeadline            time.Time
	graceCommittedTranscript string

	// Speech tracking for grace window (based on VAD energy threshold)
	speechMu         sync.Mutex
	lastUserSpeechAt time.Time
	userWasSpeaking  bool

	// Assistant output gating (used during interrupt checks to pause text delivery).
	outputMu     sync.Mutex
	outputPaused bool
	outputBuffer []SessionEvent

	// Per-turn stop reason override (set when we cancel a response).
	stopMu         sync.Mutex
	nextStopReason string

	// Interrupt check state (600ms capture + semantic classify).
	interruptMu             sync.Mutex
	interruptCheckID        uint64
	interruptPrevState      SessionState
	interruptAudioPosMs     int64
	interruptPartialAtPause string

	// Turn coordination
	turnReady chan string // Signals a new turn is ready with transcript

	// Channels
	incomingAudio  chan []byte
	incomingText   chan string
	outgoingEvents chan SessionEvent

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool

	// WebSocket write mutex
	writeMu sync.Mutex
}

// LiveSessionConfig configures a new LiveSession.
type LiveSessionConfig struct {
	Connection       *websocket.Conn
	RunStreamCreator RunStreamCreator
	STTFactory       STTStreamFactory
	TTSFactory       TTSFactory
	LLMFunc          LLMFunc
	SessionID        string
}

// NewLiveSession creates a new live session.
func NewLiveSession(cfg LiveSessionConfig) *LiveSession {
	ctx, cancel := context.WithCancel(context.Background())

	id := cfg.SessionID
	if id == "" {
		id = generateSessionID()
	}

	return &LiveSession{
		id:               id,
		conn:             cfg.Connection,
		runStreamCreator: cfg.RunStreamCreator,
		sttFactory:       cfg.STTFactory,
		ttsFactory:       cfg.TTSFactory,
		llmFunc:          cfg.LLMFunc,
		state:            StateConfiguring,
		turnReady:        make(chan string, 1),
		incomingAudio:    make(chan []byte, 100),
		incomingText:     make(chan string, 10),
		outgoingEvents:   make(chan SessionEvent, 100),
		ctx:              ctx,
		cancel:           cancel,
		done:             make(chan struct{}),
	}
}

// ID returns the session identifier.
func (s *LiveSession) ID() string {
	return s.id
}

// State returns the current session state.
func (s *LiveSession) State() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *LiveSession) setState(state SessionState) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
}

// Configure applies session configuration.
func (s *LiveSession) Configure(cfg *SessionConfig) error {
	s.stateMu.Lock()
	if s.state != StateConfiguring && s.state != StateListening {
		s.stateMu.Unlock()
		return fmt.Errorf("cannot configure in state %v", s.state)
	}
	s.stateMu.Unlock()

	cfg.ApplyDefaults()

	s.configMu.Lock()
	s.config = cfg
	s.configMu.Unlock()

	// Initialize VAD
	var vadConfig VADConfig
	if cfg.Voice.VAD != nil {
		vadConfig = *cfg.Voice.VAD
	}
	s.vad = NewHybridVAD(vadConfig)

	// Set up semantic checker for VAD
	if s.llmFunc != nil {
		semanticChecker := NewLLMSemanticChecker(s.llmFunc, LLMSemanticCheckerConfig{
			Model:   vadConfig.Model,
			Timeout: 500 * time.Millisecond,
		})
		s.vad.SetSemanticChecker(semanticChecker)
	}

	// Initialize TTS pipeline
	ttsFormat := "pcm"
	sampleRate := 24000
	if cfg.Voice.Output != nil {
		if cfg.Voice.Output.SampleRate > 0 {
			sampleRate = cfg.Voice.Output.SampleRate
		}
		if cfg.Voice.Output.Format != "" {
			ttsFormat = cfg.Voice.Output.Format
		}
	}
	s.tts = NewTTSPipeline(TTSPipelineConfig{
		SampleRate: sampleRate,
		Format:     ttsFormat,
	})

	// Initialize interrupt handler
	if s.llmFunc != nil && cfg.Voice.Interrupt != nil {
		s.interruptDetector = NewInterruptDetector(s.llmFunc, *cfg.Voice.Interrupt)
	}

	// Initialize audio buffer
	s.audioBuffer = NewAudioBuffer(DefaultAudioFormat(), 3*time.Second)

	// Initialize STT
	if s.sttFactory != nil {
		stt, err := s.sttFactory(s.ctx, cfg.Voice.Input)
		if err != nil {
			return fmt.Errorf("failed to initialize STT: %w", err)
		}
		s.sttMu.Lock()
		s.stt = stt
		s.sttMu.Unlock()
	}

	s.setState(StateListening)

	// Emit session.created after successful configuration.
	s.sendEvent(SessionCreatedEvent{
		SessionID: s.id,
		Config: SessionInfoConfig{
			Model:      s.getModel(),
			SampleRate: s.getSampleRate(),
			Channels:   1,
		},
	})
	return nil
}

// Start begins processing the session.
func (s *LiveSession) Start() {
	if s.conn != nil {
		go s.readLoop()
		go s.writeLoop()
	}
	go s.processLoop()
	go s.ttsForwardLoop()
	go s.runStreamLoop() // Single loop for RunStream lifetime
}

func (s *LiveSession) getModel() string {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.config != nil {
		return s.config.Model
	}
	return ""
}

func (s *LiveSession) getSampleRate() int {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	if s.config != nil && s.config.Voice != nil && s.config.Voice.Output != nil && s.config.Voice.Output.SampleRate > 0 {
		return s.config.Voice.Output.SampleRate
	}
	return 24000
}

// Events returns server-side session events. This is intended for in-process (direct) sessions
// where no WebSocket connection is attached.
func (s *LiveSession) Events() <-chan SessionEvent {
	return s.outgoingEvents
}

// EnqueueAudio injects an audio frame into the session (for in-process/direct mode).
func (s *LiveSession) EnqueueAudio(pcm []byte) error {
	select {
	case s.incomingAudio <- pcm:
		return nil
	case <-s.done:
		return fmt.Errorf("session closed")
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// EnqueueText injects text into the session (for in-process/direct mode).
func (s *LiveSession) EnqueueText(text string) error {
	select {
	case s.incomingText <- text:
		return nil
	case <-s.done:
		return fmt.Errorf("session closed")
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// ForceCommit forces end-of-turn (push-to-talk).
func (s *LiveSession) ForceCommit() { s.commitTurn() }

// ForceInterrupt forces an interrupt, skipping semantic check.
func (s *LiveSession) ForceInterrupt(transcript string) { s.forceInterruptAndCommitText(transcript) }

// readLoop reads messages from the WebSocket.
func (s *LiveSession) readLoop() {
	defer s.Close()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		messageType, data, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			s.sendEvent(ErrorEvent{Code: "read_error", Message: err.Error()})
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			select {
			case s.incomingAudio <- data:
			case <-s.done:
				return
			}
		case websocket.TextMessage:
			s.handleJSONMessage(data)
		}
	}
}

func (s *LiveSession) handleJSONMessage(data []byte) {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		s.sendEvent(ErrorEvent{Code: "parse_error", Message: "invalid JSON"})
		return
	}

	switch msg.Type {
	case EventTypeSessionConfigure:
		var configMsg SessionConfigureMessage
		if err := json.Unmarshal(data, &configMsg); err != nil {
			s.sendEvent(ErrorEvent{Code: "parse_error", Message: err.Error()})
			return
		}
		if err := s.Configure(configMsg.Config); err != nil {
			s.sendEvent(ErrorEvent{Code: "config_error", Message: err.Error()})
			return
		}

	case EventTypeSessionUpdate:
		var updateMsg SessionUpdateMessage
		if err := json.Unmarshal(data, &updateMsg); err != nil {
			s.sendEvent(ErrorEvent{Code: "parse_error", Message: err.Error()})
			return
		}
		if err := s.UpdateConfig(updateMsg.Config); err != nil {
			s.sendEvent(ErrorEvent{Code: "config_error", Message: err.Error()})
		}

	case EventTypeInputText:
		var textMsg InputTextMessage
		if err := json.Unmarshal(data, &textMsg); err != nil {
			s.sendEvent(ErrorEvent{Code: "parse_error", Message: err.Error()})
			return
		}
		select {
		case s.incomingText <- textMsg.Text:
		case <-s.done:
		}

	case EventTypeInputCommit:
		s.commitTurn()

	case EventTypeInputInterrupt:
		var intMsg InputInterruptMessage
		if err := json.Unmarshal(data, &intMsg); err != nil {
			s.sendEvent(ErrorEvent{Code: "parse_error", Message: err.Error()})
			return
		}
		s.forceInterruptAndCommitText(intMsg.Transcript)

	default:
		s.sendEvent(ErrorEvent{Code: "unknown_message", Message: "unknown type: " + msg.Type})
	}
}

// writeLoop writes events to the WebSocket.
func (s *LiveSession) writeLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		case event := <-s.outgoingEvents:
			s.writeEvent(event)
		}
	}
}

func (s *LiveSession) writeEvent(event SessionEvent) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if audioEvent, ok := event.(AudioDeltaEvent); ok {
		return s.conn.WriteMessage(websocket.BinaryMessage, audioEvent.Data)
	}

	msg := map[string]any{"type": event.sessionEventType()}
	eventData, _ := json.Marshal(event)
	json.Unmarshal(eventData, &msg)
	return s.conn.WriteJSON(msg)
}

// processLoop handles incoming audio and text.
func (s *LiveSession) processLoop() {
	defer s.cleanup()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		case chunk := <-s.incomingAudio:
			s.handleAudioInput(chunk)
		case text := <-s.incomingText:
			s.handleTextInput(text)
		}
	}
}

func (s *LiveSession) handleAudioInput(chunk []byte) {
	state := s.State()
	s.audioBuffer.Write(chunk)

	// Track user speech start based on the VAD energy threshold.
	now := time.Now()
	energy := CalculateEnergy(chunk)
	speechStarted := s.updateUserSpeechState(now, energy)

	// Feed to STT
	s.sttMu.Lock()
	stt := s.stt
	s.sttMu.Unlock()

	var transcriptDelta string
	if stt != nil {
		if err := stt.Write(chunk); err == nil {
			transcriptDelta = stt.TranscriptDelta()
			if transcriptDelta != "" {
				s.sendEvent(TranscriptDeltaEvent{Delta: transcriptDelta})
			}
		}
	}

	// Grace period: if we committed a user turn but they start talking again within
	// 5 seconds of their last speech, cancel the in-flight response and treat the
	// user as continuing the same utterance.
	if speechStarted && (state == StateProcessing || state == StateSpeaking) {
		if prefix, ok := s.graceTranscriptIfActive(now); ok {
			s.cancelForGrace(prefix)
			// Process this chunk as normal listening input (continue accumulating transcript).
			_ = s.vad.ProcessAudio(chunk, transcriptDelta)
			return
		}
	}

	switch state {
	case StateListening:
		result := s.vad.ProcessAudio(chunk, transcriptDelta)
		if result == VADCommit {
			s.commitTurn()
		}

	case StateProcessing, StateSpeaking:
		s.maybeBeginInterruptCheck(now, energy)

	case StateInterruptCheck:
		// During interrupt checks, we keep feeding STT (above) but don't advance VAD.
	}
}

func (s *LiveSession) vadEnergyThreshold() float64 {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	if s.config != nil && s.config.Voice != nil && s.config.Voice.VAD != nil && s.config.Voice.VAD.EnergyThreshold > 0 {
		return s.config.Voice.VAD.EnergyThreshold
	}
	return DefaultVADConfig().EnergyThreshold
}

func (s *LiveSession) updateUserSpeechState(now time.Time, energy float64) (speechStarted bool) {
	threshold := s.vadEnergyThreshold()
	isSpeech := energy >= threshold

	s.speechMu.Lock()
	defer s.speechMu.Unlock()

	speechStarted = isSpeech && !s.userWasSpeaking
	s.userWasSpeaking = isSpeech
	if isSpeech {
		s.lastUserSpeechAt = now
	}
	return speechStarted
}

func (s *LiveSession) graceTranscriptIfActive(now time.Time) (string, bool) {
	s.graceMu.Lock()
	defer s.graceMu.Unlock()

	if !s.graceActive {
		return "", false
	}
	if !s.graceDeadline.IsZero() && now.After(s.graceDeadline) {
		s.graceActive = false
		s.graceDeadline = time.Time{}
		s.graceCommittedTranscript = ""
		return "", false
	}
	if s.graceCommittedTranscript == "" {
		return "", false
	}
	return s.graceCommittedTranscript, true
}

func (s *LiveSession) cancelForGrace(prefix string) {
	// Clear grace state first so we don't re-enter.
	s.graceMu.Lock()
	s.graceActive = false
	s.graceDeadline = time.Time{}
	s.graceCommittedTranscript = ""
	s.graceMu.Unlock()

	// Drain any pending "turn ready" signal to avoid starting a response for the canceled turn.
	select {
	case <-s.turnReady:
	default:
	}

	// Stop any in-flight response without saving partial output.
	s.setNextStopReason("grace_cancelled")
	s.runStreamMu.Lock()
	runStream := s.runStream
	s.runStreamMu.Unlock()
	if runStream != nil {
		_ = runStream.StopResponse("", InterruptDiscard)
	}

	if s.tts != nil {
		s.tts.Cancel()
	}

	s.partialTextMu.Lock()
	s.partialText.Reset()
	s.partialTextMu.Unlock()

	// Reset VAD and restore the committed transcript so the user can continue seamlessly.
	prefix = strings.TrimSpace(prefix)
	s.vad.Reset()
	if prefix != "" {
		s.vad.AddTranscript(prefix + " ")
	}

	s.setState(StateListening)
}

func (s *LiveSession) handleTextInput(text string) {
	state := s.State()
	switch state {
	case StateListening:
		s.vad.AddTranscript(text)
		s.commitTurn()
	case StateSpeaking, StateProcessing, StateInterruptCheck:
		s.forceInterruptAndCommitText(text)
	}
}

func (s *LiveSession) maybeBeginInterruptCheck(now time.Time, energy float64) {
	state := s.State()
	if state != StateProcessing && state != StateSpeaking {
		return
	}

	s.configMu.RLock()
	interruptCfg := (*InterruptConfig)(nil)
	if s.config != nil && s.config.Voice != nil {
		interruptCfg = s.config.Voice.Interrupt
	}
	s.configMu.RUnlock()

	mode := InterruptModeAuto
	threshold := DefaultInterruptConfig().EnergyThreshold
	if interruptCfg != nil {
		mode = interruptCfg.Mode
		if interruptCfg.EnergyThreshold > 0 {
			threshold = interruptCfg.EnergyThreshold
		}
	}

	if mode != InterruptModeAuto {
		return
	}
	if energy < threshold {
		return
	}

	s.beginInterruptCheck(state)
}

func (s *LiveSession) beginInterruptCheck(prevState SessionState) {
	s.interruptMu.Lock()
	// Only one interrupt check at a time.
	if s.State() == StateInterruptCheck {
		s.interruptMu.Unlock()
		return
	}
	s.interruptCheckID++
	id := s.interruptCheckID
	s.interruptPrevState = prevState

	s.partialTextMu.Lock()
	s.interruptPartialAtPause = s.partialText.String()
	s.partialTextMu.Unlock()

	audioPos := int64(0)
	if s.tts != nil {
		audioPos = s.tts.Pause()
	}
	s.interruptAudioPosMs = audioPos
	s.interruptMu.Unlock()

	s.setOutputPaused(true)
	s.setState(StateInterruptCheck)

	go s.finishInterruptCheck(id)
}

func (s *LiveSession) finishInterruptCheck(id uint64) {
	select {
	case <-s.ctx.Done():
		return
	case <-s.done:
		return
	case <-time.After(600 * time.Millisecond):
	}

	s.interruptMu.Lock()
	if s.interruptCheckID != id || s.State() != StateInterruptCheck {
		s.interruptMu.Unlock()
		return
	}
	prevState := s.interruptPrevState
	audioPos := s.interruptAudioPosMs
	partialAtPause := s.interruptPartialAtPause
	s.interruptMu.Unlock()

	interruptTranscript := s.currentSTTTranscript()
	interruptTranscript = strings.TrimSpace(interruptTranscript)
	if interruptTranscript == "" {
		s.dismissInterruptCheck("", "no_transcription", prevState)
		return
	}

	// Notify client that we're semantically classifying.
	s.sendEvent(InterruptDetectingEvent{Transcript: interruptTranscript})

	isInterrupt := true
	reason := "interrupt"
	if s.interruptDetector != nil {
		result := s.interruptDetector.CheckInterrupt(s.ctx, interruptTranscript)
		isInterrupt = result.IsInterrupt
		reason = result.Reason
	}

	if !isInterrupt {
		s.dismissInterruptCheck(interruptTranscript, reason, prevState)
		return
	}

	s.confirmAudioInterrupt(interruptTranscript, partialAtPause, audioPos)
}

func (s *LiveSession) dismissInterruptCheck(transcript string, reason string, prevState SessionState) {
	// Clear STT transcript so future interrupts only consider new speech.
	s.sttMu.Lock()
	if s.stt != nil {
		s.stt.Reset()
	}
	s.sttMu.Unlock()

	s.setState(prevState)
	s.setOutputPaused(false)
	s.flushOutputBuffer()

	if s.tts != nil {
		s.tts.Resume()
	}

	if strings.TrimSpace(transcript) != "" {
		s.sendEvent(InterruptDismissedEvent{Transcript: transcript, Reason: reason})
	}
}

func (s *LiveSession) confirmAudioInterrupt(interruptTranscript string, partialAtPause string, audioPosMs int64) {
	// Stop the in-flight response and persist partial assistant output per config.
	behavior := s.interruptBehavior()
	s.setNextStopReason("interrupted")

	s.runStreamMu.Lock()
	runStream := s.runStream
	s.runStreamMu.Unlock()
	if runStream != nil {
		_ = runStream.StopResponse(partialAtPause, behavior)
	}

	// Cancel audio output and discard any buffered chunks.
	if s.tts != nil {
		s.tts.Cancel()
	}

	// Drop any buffered assistant events produced while paused.
	s.clearOutputBuffer()

	s.sendEvent(ResponseInterruptedEvent{
		PartialText:         partialAtPause,
		InterruptTranscript: interruptTranscript,
		AudioPositionMs:     int(audioPosMs),
	})

	// Reset grace state and resume listening for the user's full interrupt utterance.
	s.graceMu.Lock()
	s.graceActive = false
	s.graceDeadline = time.Time{}
	s.graceCommittedTranscript = ""
	s.graceMu.Unlock()

	s.vad.Reset()
	if strings.TrimSpace(interruptTranscript) != "" {
		s.vad.AddTranscript(strings.TrimSpace(interruptTranscript) + " ")
	}

	s.setOutputPaused(false)
	s.setState(StateListening)
}

func (s *LiveSession) forceInterruptAndCommitText(transcript string) {
	transcript = strings.TrimSpace(transcript)

	s.partialTextMu.Lock()
	partialText := s.partialText.String()
	s.partialTextMu.Unlock()

	audioPos := int64(0)
	if s.tts != nil {
		audioPos = s.tts.Cancel()
	}

	s.setNextStopReason("interrupted")

	s.runStreamMu.Lock()
	runStream := s.runStream
	s.runStreamMu.Unlock()
	if runStream != nil {
		_ = runStream.StopResponse(partialText, s.interruptBehavior())
	}

	s.clearOutputBuffer()
	s.setOutputPaused(false)

	s.sendEvent(ResponseInterruptedEvent{
		PartialText:         partialText,
		InterruptTranscript: transcript,
		AudioPositionMs:     int(audioPos),
	})

	s.sttMu.Lock()
	if s.stt != nil {
		s.stt.Reset()
	}
	s.sttMu.Unlock()

	s.vad.Reset()
	if transcript != "" {
		s.vad.AddTranscript(transcript)
	}

	s.setState(StateListening)

	if transcript != "" {
		s.commitTurn()
	}
}

func (s *LiveSession) currentSTTTranscript() string {
	s.sttMu.Lock()
	defer s.sttMu.Unlock()
	if s.stt == nil {
		return ""
	}
	return s.stt.Transcript()
}

func (s *LiveSession) interruptBehavior() InterruptBehavior {
	s.configMu.RLock()
	defer s.configMu.RUnlock()

	if s.config == nil || s.config.Voice == nil || s.config.Voice.Interrupt == nil {
		return InterruptSaveMarked
	}
	switch s.config.Voice.Interrupt.SavePartial {
	case SaveBehaviorDiscard:
		return InterruptDiscard
	case SaveBehaviorSave:
		return InterruptSavePartial
	case SaveBehaviorMarked:
		return InterruptSaveMarked
	default:
		return InterruptSaveMarked
	}
}

func (s *LiveSession) setOutputPaused(paused bool) {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	s.outputPaused = paused
	if paused {
		s.outputBuffer = s.outputBuffer[:0]
	}
}

func (s *LiveSession) clearOutputBuffer() {
	s.outputMu.Lock()
	s.outputBuffer = s.outputBuffer[:0]
	s.outputPaused = false
	s.outputMu.Unlock()
}

func (s *LiveSession) flushOutputBuffer() {
	s.outputMu.Lock()
	buf := make([]SessionEvent, len(s.outputBuffer))
	copy(buf, s.outputBuffer)
	s.outputBuffer = s.outputBuffer[:0]
	s.outputMu.Unlock()

	for _, evt := range buf {
		s.sendEvent(evt)
	}
}

// commitTurn commits the current turn.
func (s *LiveSession) commitTurn() {
	s.stateMu.Lock()
	if s.state != StateListening {
		s.stateMu.Unlock()
		return
	}
	s.state = StateProcessing
	s.stateMu.Unlock()

	transcript := strings.TrimSpace(s.vad.GetTranscript())

	s.sttMu.Lock()
	if transcript == "" && s.stt != nil {
		transcript = strings.TrimSpace(s.stt.Transcript())
	}
	if s.stt != nil {
		s.stt.Reset()
	}
	s.sttMu.Unlock()

	if transcript == "" {
		s.setState(StateListening)
		return
	}

	// Start a user grace window anchored to the last detected user speech.
	s.speechMu.Lock()
	lastSpeech := s.lastUserSpeechAt
	s.speechMu.Unlock()

	if !lastSpeech.IsZero() {
		s.graceMu.Lock()
		s.graceActive = true
		s.graceDeadline = lastSpeech.Add(userGracePeriod)
		s.graceCommittedTranscript = transcript
		s.graceMu.Unlock()
	}

	s.vad.Reset()
	s.sendEvent(InputCommittedEvent{Transcript: transcript})

	// Signal the runStreamLoop
	select {
	case s.turnReady <- transcript:
	default:
		// If channel is full, the previous turn is still processing
	}
}

// runStreamLoop manages the RunStream for the session lifetime.
// First turn: create RunStream
// Subsequent turns: use Interrupt() to inject new messages
func (s *LiveSession) runStreamLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		case transcript := <-s.turnReady:
			s.processTurn(transcript)
		}
	}
}

func (s *LiveSession) processTurn(transcript string) {
	s.runStreamMu.Lock()
	runStream := s.runStream
	s.runStreamMu.Unlock()

	if runStream == nil {
		// First turn: create RunStream
		if s.runStreamCreator == nil {
			s.setState(StateListening)
			s.sendEvent(ErrorEvent{Code: "no_run_stream", Message: "RunStreamCreator not configured"})
			return
		}

		s.configMu.RLock()
		config := s.config
		s.configMu.RUnlock()

		stream, err := s.runStreamCreator(s.ctx, config, transcript)
		if err != nil {
			s.setState(StateListening)
			s.sendEvent(ErrorEvent{Code: "run_stream_error", Message: err.Error()})
			return
		}

		s.runStreamMu.Lock()
		s.runStream = stream
		s.runStreamMu.Unlock()

		// Start processing events from this stream
		go s.processRunStreamEvents(stream)
	} else {
		// Subsequent turn: inject via Interrupt
		err := runStream.Interrupt(UserMessage{Role: "user", Content: transcript}, InterruptDiscard)
		if err != nil {
			// RunStream may have closed, try creating a new one
			s.runStreamMu.Lock()
			s.runStream = nil
			s.runStreamMu.Unlock()
			s.processTurn(transcript) // Retry with new stream
		}
	}
}

// processRunStreamEvents processes events from the RunStream.
// This runs for the lifetime of the RunStream.
func (s *LiveSession) processRunStreamEvents(stream RunStreamInterface) {
	defer func() {
		s.runStreamMu.Lock()
		if s.runStream == stream {
			s.runStream = nil
		}
		s.runStreamMu.Unlock()
	}()

	s.partialTextMu.Lock()
	s.partialText.Reset()
	s.partialTextMu.Unlock()

	if s.tts != nil {
		s.tts.Reset()
	}

	for event := range stream.Events() {
		select {
		case <-s.ctx.Done():
			stream.Close()
			return
		case <-s.done:
			stream.Close()
			return
		default:
		}

		s.forwardRunStreamEvent(event)
	}

	// Stream ended
	s.setState(StateListening)

	if s.tts != nil {
		s.tts.Flush()
	}

	s.sendEvent(MessageStopEvent{StopReason: "end_turn"})
	s.sendEvent(VADListeningEvent{})
}

// forwardRunStreamEvent forwards events from RunStream to client and TTS.
func (s *LiveSession) forwardRunStreamEvent(event RunStreamEvent) {
	eventType := event.runStreamEventType()

	switch eventType {
	case "stream_event":
		// Unwrap and process underlying stream event
		if wrapper, ok := event.(interface{ Event() any }); ok {
			s.processStreamEvent(wrapper.Event())
		}

	case "step_start":
		// New assistant turn.
		s.partialTextMu.Lock()
		s.partialText.Reset()
		s.partialTextMu.Unlock()
		s.setNextStopReason("")

		// Start a new TTS streaming context for this assistant turn (if configured).
		s.startTTSTurn()
		s.sendAssistantEvent(ContentBlockStartEvent{Index: 0, ContentBlock: map[string]any{"type": "text", "text": ""}})

	case "step_complete":
		s.sendAssistantEvent(ContentBlockStopEvent{Index: 0})

		stopReason := s.consumeNextStopReason("end_turn")
		if s.tts != nil && stopReason == "end_turn" {
			s.tts.Flush()
		}

		s.sendAssistantEvent(MessageStopEvent{StopReason: stopReason})
		s.sendAssistantEvent(VADListeningEvent{})
		if s.State() != StateListening {
			s.setState(StateListening)
		}

	case "tool_call_start":
		if tc, ok := event.(interface {
			ToolID() string
			ToolName() string
			ToolInput() map[string]any
		}); ok {
			s.sendAssistantEvent(ToolUseEvent{
				ID:    tc.ToolID(),
				Name:  tc.ToolName(),
				Input: tc.ToolInput(),
			})
		}

	case "interrupted":
		// RunStream was interrupted, clear partial and reset TTS
		s.partialTextMu.Lock()
		s.partialText.Reset()
		s.partialTextMu.Unlock()
		if s.tts != nil {
			s.tts.Reset()
		}

	case "run_complete":
		// Will be handled by the for loop ending
	}
}

func (s *LiveSession) sendAssistantEvent(event SessionEvent) {
	s.outputMu.Lock()
	paused := s.outputPaused
	if paused {
		s.outputBuffer = append(s.outputBuffer, event)
		s.outputMu.Unlock()
		return
	}
	s.outputMu.Unlock()
	s.sendEvent(event)
}

func (s *LiveSession) setNextStopReason(reason string) {
	s.stopMu.Lock()
	s.nextStopReason = reason
	s.stopMu.Unlock()
}

func (s *LiveSession) consumeNextStopReason(defaultReason string) string {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.nextStopReason == "" {
		return defaultReason
	}
	reason := s.nextStopReason
	s.nextStopReason = ""
	return reason
}

func (s *LiveSession) startTTSTurn() {
	s.configMu.RLock()
	outputCfg := (*VoiceOutputConfig)(nil)
	if s.config != nil && s.config.Voice != nil {
		outputCfg = s.config.Voice.Output
	}
	s.configMu.RUnlock()

	if outputCfg == nil || s.ttsFactory == nil || s.tts == nil {
		return
	}

	ttsCtx, err := s.ttsFactory(s.ctx, outputCfg)
	if err != nil {
		s.sendEvent(ErrorEvent{Code: "tts_init_error", Message: err.Error()})
		return
	}
	s.tts.SetTTSContext(ttsCtx)
}

// processStreamEvent handles underlying stream events (text deltas, etc.)
func (s *LiveSession) processStreamEvent(event any) {
	// Check for content block delta with text
	if delta, ok := event.(interface{ TextDelta() (string, bool) }); ok {
		if text, hasText := delta.TextDelta(); hasText && text != "" {
			s.partialTextMu.Lock()
			s.partialText.WriteString(text)
			s.partialTextMu.Unlock()

			if s.State() == StateProcessing {
				s.setState(StateSpeaking)
			}

			if s.tts != nil {
				s.tts.SendText(text)
			}

			s.sendAssistantEvent(ContentBlockDeltaEvent{
				Index: 0,
				Delta: map[string]string{"type": "text_delta", "text": text},
			})
		}
	}
}

// ttsForwardLoop forwards TTS audio to the client.
func (s *LiveSession) ttsForwardLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		if s.tts == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		select {
		case chunk, ok := <-s.tts.Audio():
			if !ok {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			s.sendEvent(AudioDeltaEvent{Data: chunk})
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		}
	}
}

// UpdateConfig updates session configuration mid-session.
func (s *LiveSession) UpdateConfig(cfg *SessionConfig) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	if s.config == nil {
		return fmt.Errorf("session not configured")
	}

	if cfg.Model != "" {
		s.config.Model = cfg.Model
	}
	if cfg.System != "" {
		s.config.System = cfg.System
	}
	if cfg.Tools != nil {
		s.config.Tools = cfg.Tools
	}
	if cfg.Voice != nil {
		if cfg.Voice.VAD != nil && s.vad != nil {
			s.vad.UpdateConfig(*cfg.Voice.VAD)
		}
		if cfg.Voice.Output != nil {
			s.config.Voice.Output = cfg.Voice.Output
		}
		if cfg.Voice.Interrupt != nil {
			s.config.Voice.Interrupt = cfg.Voice.Interrupt
		}
	}

	return nil
}

func (s *LiveSession) sendEvent(event SessionEvent) {
	if s.closed.Load() {
		return
	}
	select {
	case s.outgoingEvents <- event:
	case <-s.done:
	default:
	}
}

func (s *LiveSession) cleanup() {
	s.runStreamMu.Lock()
	if s.runStream != nil {
		s.runStream.Close()
		s.runStream = nil
	}
	s.runStreamMu.Unlock()

	s.sttMu.Lock()
	if s.stt != nil {
		s.stt.Close()
		s.stt = nil
	}
	s.sttMu.Unlock()

	if s.tts != nil {
		s.tts.Close()
	}
}

// Close closes the session.
func (s *LiveSession) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.cancel()
		close(s.done)
		s.cleanup()
		if s.conn != nil {
			s.conn.Close()
		}
		s.setState(StateClosed)
	})
	return nil
}

// Done returns a channel that's closed when the session is done.
func (s *LiveSession) Done() <-chan struct{} {
	return s.done
}

var sessionCounter atomic.Uint64

func generateSessionID() string {
	return fmt.Sprintf("live_%d_%d", time.Now().UnixNano(), sessionCounter.Add(1))
}

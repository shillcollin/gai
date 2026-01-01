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
	llmFunc          LLMFunc

	// Active components
	runStream   RunStreamInterface
	runStreamMu sync.Mutex
	stt         STTStream
	sttMu       sync.Mutex
	vad         *HybridVAD
	tts         *TTSPipeline
	interrupt   *InterruptHandler
	audioBuffer *AudioBuffer

	// State
	state   SessionState
	stateMu sync.RWMutex

	// Partial text tracking for interrupts
	partialText   strings.Builder
	partialTextMu sync.Mutex

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
	if cfg.Voice.Output != nil && cfg.Voice.Output.Format != "" {
		ttsFormat = cfg.Voice.Output.Format
	}
	s.tts = NewTTSPipeline(TTSPipelineConfig{
		SampleRate: 24000,
		Format:     ttsFormat,
	})

	// Initialize interrupt handler
	if s.llmFunc != nil && cfg.Voice.Interrupt != nil {
		interruptDetector := NewInterruptDetector(s.llmFunc, *cfg.Voice.Interrupt)
		s.interrupt = NewInterruptHandler(InterruptHandlerConfig{
			Pipeline: s.tts,
			Detector: interruptDetector,
			SaveMode: cfg.Voice.Interrupt.SavePartial,
			OnInterruptDetecting: func(transcript string) {
				s.sendEvent(InterruptDetectingEvent{Transcript: transcript})
			},
			OnInterruptDismissed: func(transcript string, reason string) {
				s.sendEvent(InterruptDismissedEvent{Transcript: transcript, Reason: reason})
			},
			OnInterruptConfirmed: func(partialText, interruptTranscript string, audioPositionMs int64) {
				s.sendEvent(ResponseInterruptedEvent{
					PartialText:         partialText,
					InterruptTranscript: interruptTranscript,
					AudioPositionMs:     int(audioPositionMs),
				})
			},
		})
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
	return nil
}

// Start begins processing the session.
func (s *LiveSession) Start() {
	go s.readLoop()
	go s.writeLoop()
	go s.processLoop()
	go s.ttsForwardLoop()
	go s.runStreamLoop() // Single loop for RunStream lifetime

	s.sendEvent(SessionCreatedEvent{
		SessionID: s.id,
		Model:     s.getModel(),
	})
}

func (s *LiveSession) getModel() string {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.config != nil {
		return s.config.Model
	}
	return ""
}

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
		s.sendEvent(SessionCreatedEvent{SessionID: s.id, Model: configMsg.Config.Model})

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
		s.forceInterrupt(intMsg.Transcript)

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

	switch state {
	case StateListening:
		result := s.vad.ProcessAudio(chunk, transcriptDelta)
		if result == VADCommit {
			s.commitTurn()
		}

	case StateSpeaking:
		s.configMu.RLock()
		mode := InterruptModeAuto
		if s.config != nil && s.config.Voice.Interrupt != nil {
			mode = s.config.Voice.Interrupt.Mode
		}
		s.configMu.RUnlock()

		if mode == InterruptModeAuto {
			s.handlePotentialInterrupt(chunk, transcriptDelta)
		}
	}
}

func (s *LiveSession) handleTextInput(text string) {
	state := s.State()
	if state == StateSpeaking || state == StateProcessing {
		s.forceInterrupt(text)
		return
	}
	if state == StateListening {
		s.vad.AddTranscript(text)
		s.commitTurn()
	}
}

func (s *LiveSession) handlePotentialInterrupt(chunk []byte, transcript string) {
	s.configMu.RLock()
	threshold := 0.05
	debounceMs := 100
	if s.config != nil && s.config.Voice.Interrupt != nil {
		if s.config.Voice.Interrupt.EnergyThreshold > 0 {
			threshold = s.config.Voice.Interrupt.EnergyThreshold
		}
		if s.config.Voice.Interrupt.DebounceMs > 0 {
			debounceMs = s.config.Voice.Interrupt.DebounceMs
		}
	}
	s.configMu.RUnlock()

	energy := CalculateEnergy(chunk)
	if energy < threshold {
		return
	}

	if !s.audioBuffer.HasSustainedEnergy(threshold, time.Duration(debounceMs)*time.Millisecond) {
		return
	}

	s.sttMu.Lock()
	fullTranscript := ""
	if s.stt != nil {
		fullTranscript = s.stt.Transcript()
	}
	s.sttMu.Unlock()

	if fullTranscript == "" && transcript != "" {
		fullTranscript = transcript
	}
	if fullTranscript == "" {
		return
	}

	s.setState(StateInterruptCheck)

	s.partialTextMu.Lock()
	partialText := s.partialText.String()
	s.partialTextMu.Unlock()

	if s.interrupt != nil {
		go func() {
			result := s.interrupt.HandlePotentialInterrupt(s.ctx, partialText, fullTranscript)
			if result.IsInterrupt {
				s.confirmInterrupt(fullTranscript)
			} else {
				s.dismissInterrupt(fullTranscript, result.Reason)
			}
		}()
	} else {
		s.confirmInterrupt(fullTranscript)
	}
}

func (s *LiveSession) confirmInterrupt(transcript string) {
	s.setState(StateListening)

	// TTS is already cancelled by interrupt handler
	if s.tts != nil {
		s.tts.Cancel()
	}

	// Use RunStream.InterruptWithText to inject new turn
	s.runStreamMu.Lock()
	if s.runStream != nil {
		s.runStream.InterruptWithText(transcript)
	}
	s.runStreamMu.Unlock()

	s.partialTextMu.Lock()
	s.partialText.Reset()
	s.partialTextMu.Unlock()

	s.vad.Reset()
	s.vad.AddTranscript(transcript)

	s.sttMu.Lock()
	if s.stt != nil {
		s.stt.Reset()
	}
	s.sttMu.Unlock()
}

func (s *LiveSession) dismissInterrupt(transcript string, reason string) {
	s.setState(StateSpeaking)
	// TTS is resumed by interrupt handler
}

func (s *LiveSession) forceInterrupt(transcript string) {
	s.partialTextMu.Lock()
	partialText := s.partialText.String()
	s.partialTextMu.Unlock()

	if s.interrupt != nil {
		s.interrupt.ForceInterrupt(partialText, transcript)
	}
	s.confirmInterrupt(transcript)
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

	transcript := s.vad.GetTranscript()

	s.sttMu.Lock()
	if s.stt != nil {
		sttTranscript := s.stt.Transcript()
		if sttTranscript != "" && sttTranscript != transcript {
			if transcript == "" {
				transcript = sttTranscript
			} else {
				transcript = transcript + " " + sttTranscript
			}
		}
		s.stt.Reset()
	}
	s.sttMu.Unlock()

	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		s.setState(StateListening)
		return
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
		s.sendEvent(ContentBlockStartEvent{Index: 0})

	case "step_complete":
		s.sendEvent(ContentBlockStopEvent{Index: 0})

	case "tool_call_start":
		if tc, ok := event.(interface {
			ToolID() string
			ToolName() string
			ToolInput() map[string]any
		}); ok {
			s.sendEvent(ToolUseEvent{
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

			s.sendEvent(ContentBlockDeltaEvent{
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

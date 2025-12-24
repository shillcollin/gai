package gai

import (
	"context"
	"fmt"
	"sync"

	"github.com/shillcollin/gai/core"
)

// RealtimeSession provides a session-based interface for continuous voice interaction.
// It maintains a conversation and handles bidirectional audio/text streaming.
//
// The initial implementation uses the voice pipeline (STT → LLM → TTS) under the hood.
// Future versions will support native realtime APIs (OpenAI Realtime, Gemini Live)
// when available for the configured model.
//
// Example:
//
//	session, err := client.RealtimeSession(ctx, gai.RealtimeConfig{
//	    Model: "anthropic/claude-3-5-sonnet",
//	    Voice: "elevenlabs/rachel",
//	    STT:   "deepgram/nova-2",
//	})
//	if err != nil {
//	    return err
//	}
//	defer session.Close()
//
//	// Send audio
//	go func() {
//	    for chunk := range microphoneChunks {
//	        session.SendAudio(chunk)
//	    }
//	}()
//
//	// Receive events
//	for event := range session.Events() {
//	    switch event.Type {
//	    case core.EventTranscript:
//	        fmt.Println("User:", event.Transcript)
//	    case core.EventTextDelta:
//	        fmt.Print(event.TextDelta)
//	    case core.EventAudioDelta:
//	        speaker.Write(event.AudioDelta)
//	    }
//	}
type RealtimeSession struct {
	mu     sync.RWMutex
	client *Client
	config RealtimeConfig
	conv   *Conversation

	ctx    context.Context
	cancel context.CancelFunc

	// Event channels
	events    chan core.StreamEvent
	audioIn   chan []byte
	textIn    chan string

	// State
	closed      bool
	interrupted bool
	processing  bool

	// Current stream (if actively generating)
	currentStream *RealtimeVoiceStream
}

// RealtimeConfig configures a RealtimeSession.
type RealtimeConfig struct {
	// Model specifies the LLM model (e.g., "anthropic/claude-3-5-sonnet").
	Model string

	// Voice specifies the TTS voice (e.g., "elevenlabs/rachel").
	Voice string

	// STT specifies the STT provider (e.g., "deepgram/nova-2").
	STT string

	// Tools are available for the session.
	Tools []core.ToolHandle

	// System is the system prompt.
	System string

	// InputMode controls what input types are accepted.
	InputMode InputMode

	// VADEnabled enables Voice Activity Detection (future feature).
	VADEnabled bool

	// MaxMessages limits conversation history (0 = unlimited).
	MaxMessages int
}

// InputMode specifies what input types the session accepts.
type InputMode string

const (
	// InputModeAudio accepts only audio input.
	InputModeAudio InputMode = "audio"
	// InputModeText accepts only text input.
	InputModeText InputMode = "text"
	// InputModeBoth accepts both audio and text input.
	InputModeBoth InputMode = "both"
)

// RealtimeSession creates a new realtime session for continuous voice interaction.
//
// The session maintains conversation history and provides a streaming interface
// for sending audio/text and receiving events (transcripts, text deltas, audio deltas).
//
// Example:
//
//	session, err := client.RealtimeSession(ctx, gai.RealtimeConfig{
//	    Model: "anthropic/claude-3-5-sonnet",
//	    Voice: "elevenlabs/rachel",
//	})
//	defer session.Close()
//
//	session.SendText("Hello!")
//	for event := range session.Events() {
//	    // Handle events
//	}
func (c *Client) RealtimeSession(ctx context.Context, cfg RealtimeConfig) (*RealtimeSession, error) {
	// Validate config
	if cfg.Model == "" {
		if c.defaults.Model != "" {
			cfg.Model = c.defaults.Model
		} else {
			return nil, fmt.Errorf("model required for realtime session")
		}
	}
	if cfg.Voice == "" {
		cfg.Voice = c.defaults.Voice
	}
	if cfg.STT == "" {
		cfg.STT = c.defaults.STT
	}
	if cfg.InputMode == "" {
		cfg.InputMode = InputModeBoth
	}

	// Create cancellable context
	sessionCtx, cancel := context.WithCancel(ctx)

	// Create underlying conversation
	convOpts := []ConversationOption{
		ConvModel(cfg.Model),
	}
	if cfg.System != "" {
		convOpts = append(convOpts, ConvSystem(cfg.System))
	}
	if cfg.Voice != "" {
		convOpts = append(convOpts, ConvVoice(cfg.Voice))
	}
	if cfg.STT != "" {
		convOpts = append(convOpts, ConvSTT(cfg.STT))
	}
	if len(cfg.Tools) > 0 {
		convOpts = append(convOpts, ConvTools(cfg.Tools...))
	}
	if cfg.MaxMessages > 0 {
		convOpts = append(convOpts, ConvMaxMessages(cfg.MaxMessages))
	}

	session := &RealtimeSession{
		client:  c,
		config:  cfg,
		conv:    c.Conversation(convOpts...),
		ctx:     sessionCtx,
		cancel:  cancel,
		events:  make(chan core.StreamEvent, 100),
		audioIn: make(chan []byte, 100),
		textIn:  make(chan string, 100),
	}

	// Start input processor
	go session.processInputs()

	return session, nil
}

// SendAudio sends audio data to the session.
// The audio will be transcribed and processed as user input.
func (s *RealtimeSession) SendAudio(data []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSessionClosed
	}
	if s.config.InputMode == InputModeText {
		return fmt.Errorf("session configured for text-only input")
	}

	select {
	case s.audioIn <- data:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// SendText sends text to the session.
// The text will be processed as user input.
func (s *RealtimeSession) SendText(text string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSessionClosed
	}
	if s.config.InputMode == InputModeAudio {
		return fmt.Errorf("session configured for audio-only input")
	}

	select {
	case s.textIn <- text:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Events returns the channel of streaming events.
// Events include transcripts, text deltas, audio deltas, tool calls, etc.
func (s *RealtimeSession) Events() <-chan core.StreamEvent {
	return s.events
}

// Interrupt stops the current response generation.
// Use this when the user starts speaking to cut off the assistant.
func (s *RealtimeSession) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSessionClosed
	}

	s.interrupted = true

	// Close current stream if active
	if s.currentStream != nil {
		s.currentStream.Close()
		s.currentStream = nil
	}

	// Send interrupt event
	select {
	case s.events <- core.StreamEvent{Type: core.EventType("interrupt")}:
	default:
	}

	return nil
}

// Close closes the session and releases resources.
func (s *RealtimeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.cancel()

	// Close current stream
	if s.currentStream != nil {
		s.currentStream.Close()
	}

	// Close channels
	close(s.events)

	return nil
}

// Conversation returns the underlying conversation for direct access.
func (s *RealtimeSession) Conversation() *Conversation {
	return s.conv
}

// processInputs handles incoming audio and text inputs.
func (s *RealtimeSession) processInputs() {
	for {
		select {
		case <-s.ctx.Done():
			return

		case audio, ok := <-s.audioIn:
			if !ok {
				return
			}
			s.handleAudioInput(audio)

		case text, ok := <-s.textIn:
			if !ok {
				return
			}
			s.handleTextInput(text)
		}
	}
}

// handleAudioInput processes audio input.
func (s *RealtimeSession) handleAudioInput(audio []byte) {
	s.mu.Lock()
	if s.processing {
		s.mu.Unlock()
		return // Already processing, queue or drop
	}
	s.processing = true
	s.interrupted = false
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.processing = false
		s.mu.Unlock()
	}()

	// Use StreamVoiceRealtime for audio input
	stream, err := s.client.StreamVoiceRealtime(s.ctx,
		Request(s.config.Model).
			Audio(audio, "audio/wav").
			STT(s.config.STT).
			Voice(s.config.Voice).
			Tools(s.config.Tools...))

	if err != nil {
		s.sendError(err)
		return
	}

	s.mu.Lock()
	s.currentStream = stream
	s.mu.Unlock()

	// Send transcript event
	if stream.Transcript() != "" {
		s.sendEvent(core.StreamEvent{
			Type:       core.EventType("transcript"),
			TextDelta:  stream.Transcript(), // Using TextDelta field for transcript
		})
	}

	// Forward LLM events
	for event := range stream.LLMEvents() {
		if s.isInterrupted() {
			break
		}
		s.sendEvent(event)
	}

	// Forward TTS events (if voice enabled)
	if stream.TTSEvents() != nil {
		for ttsEvent := range stream.TTSEvents() {
			if s.isInterrupted() {
				break
			}
			// Convert TTS event to StreamEvent using Ext for audio data
			s.sendEvent(core.StreamEvent{
				Type: core.EventAudioDelta,
				Ext: map[string]any{
					"audio_data": ttsEvent.Data,
				},
			})
		}
	}

	stream.Close()

	s.mu.Lock()
	s.currentStream = nil
	s.mu.Unlock()

	// Update conversation history
	// Note: StreamVoiceRealtime doesn't auto-update history, so we need to do it manually
	// This is a simplified approach - a full implementation would collect the full response
}

// handleTextInput processes text input.
func (s *RealtimeSession) handleTextInput(text string) {
	s.mu.Lock()
	if s.processing {
		s.mu.Unlock()
		return
	}
	s.processing = true
	s.interrupted = false
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.processing = false
		s.mu.Unlock()
	}()

	// Build request
	req := Request(s.config.Model).
		User(text).
		Tools(s.config.Tools...)

	if s.config.Voice != "" {
		req = req.Voice(s.config.Voice)
	}

	// Use conversation for history management
	stream, err := s.conv.Stream(s.ctx, text)
	if err != nil {
		s.sendError(err)
		return
	}

	// Forward events
	for event := range stream.Events() {
		if s.isInterrupted() {
			break
		}
		s.sendEvent(event)
	}

	stream.Close()

	// If voice is enabled, synthesize after text is complete
	// (simplified - real implementation would stream TTS)
	if s.config.Voice != "" {
		result := stream.Result()
		if result != nil && result.HasText() {
			audio, err := s.client.Synthesize(s.ctx, result.Text(), WithTTS(s.config.Voice))
			if err == nil && len(audio) > 0 {
				// Send audio as single chunk (simplified)
				s.sendEvent(core.StreamEvent{
					Type: core.EventAudioDelta,
					Ext: map[string]any{
						"audio_data": audio,
					},
				})
			}
		}
	}
}

func (s *RealtimeSession) isInterrupted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interrupted || s.closed
}

func (s *RealtimeSession) sendEvent(event core.StreamEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	default:
		// Channel full, drop event
	}
}

func (s *RealtimeSession) sendError(err error) {
	s.sendEvent(core.StreamEvent{
		Type:  core.EventError,
		Error: err,
	})
}

// ErrSessionClosed is returned when operations are attempted on a closed session.
var ErrSessionClosed = fmt.Errorf("realtime session is closed")

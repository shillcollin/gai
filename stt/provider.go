// Package stt provides Speech-to-Text provider interfaces and types.
package stt

import (
	"context"
	"io"
	"time"
)

// Provider is the interface for speech-to-text providers.
type Provider interface {
	// Transcribe converts audio bytes to text.
	Transcribe(ctx context.Context, audio []byte, opts Options) (*Transcript, error)

	// StreamTranscribe streams audio and returns transcription events.
	// This is useful for real-time transcription of audio streams.
	StreamTranscribe(ctx context.Context, audio io.Reader, opts Options) (*TranscriptStream, error)

	// Capabilities returns the provider's capabilities.
	Capabilities() Capabilities
}

// Capabilities describes the features supported by an STT provider.
type Capabilities struct {
	Provider    string   // Provider identifier (e.g., "deepgram")
	Models      []string // Available models
	Languages   []string // Supported language codes
	Realtime    bool     // Supports real-time streaming
	Diarization bool     // Supports speaker diarization
	Timestamps  bool     // Supports word-level timestamps
	MaxDuration int      // Max audio duration in seconds (0 = unlimited)
}

// Options configures a transcription request.
type Options struct {
	Model      string         // Model identifier (e.g., "nova-2")
	Language   string         // Language code (e.g., "en", "en-US")
	Diarize    bool           // Enable speaker diarization
	Timestamps bool           // Include word timestamps
	Vocabulary []string       // Custom vocabulary/keywords
	Custom     map[string]any // Provider-specific options
}

// Transcript represents a transcription result.
type Transcript struct {
	Text       string    // Full transcribed text
	Words      []Word    // Word-level details (if requested)
	Speakers   []Speaker // Speaker segments (if diarized)
	Language   string    // Detected/used language
	Confidence float64   // Overall confidence score (0-1)
	Duration   float64   // Audio duration in seconds
	Model      string    // Model used
	Provider   string    // Provider used
}

// Word represents a single transcribed word with timing information.
type Word struct {
	Text       string  // The word text
	Start      float64 // Start time in seconds
	End        float64 // End time in seconds
	Confidence float64 // Word confidence (0-1)
	Speaker    int     // Speaker ID (if diarized, -1 if not)
}

// Speaker represents a speaker segment in diarized transcription.
type Speaker struct {
	ID    int     // Speaker identifier (0-indexed)
	Start float64 // Segment start time in seconds
	End   float64 // Segment end time in seconds
	Text  string  // Speaker's text in this segment
}

// TranscriptStream represents a streaming transcription session.
type TranscriptStream struct {
	events chan TranscriptEvent
	err    error
	done   chan struct{}
}

// NewTranscriptStream creates a new transcript stream.
func NewTranscriptStream() *TranscriptStream {
	return &TranscriptStream{
		events: make(chan TranscriptEvent, 100),
		done:   make(chan struct{}),
	}
}

// Events returns the channel of transcription events.
func (s *TranscriptStream) Events() <-chan TranscriptEvent {
	return s.events
}

// Send sends an event to the stream. Returns false if stream is closed.
func (s *TranscriptStream) Send(event TranscriptEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.done:
		return false
	}
}

// Close closes the stream.
func (s *TranscriptStream) Close() error {
	select {
	case <-s.done:
		// Already closed
	default:
		close(s.done)
		close(s.events)
	}
	return nil
}

// Err returns any error that occurred during streaming.
func (s *TranscriptStream) Err() error {
	return s.err
}

// SetError sets the stream error.
func (s *TranscriptStream) SetError(err error) {
	s.err = err
}

// TranscriptEvent represents a streaming transcription event.
type TranscriptEvent struct {
	Type       TranscriptEventType // Event type
	Partial    string              // Partial transcript (interim result)
	Final      string              // Final transcript segment
	Words      []Word              // Words in this segment
	IsFinal    bool                // Is this a final result?
	Confidence float64             // Confidence for this segment
	Error      error               // Error (if Type == TranscriptEventError)
	Timestamp  time.Time           // When this event was received
}

// TranscriptEventType identifies the type of transcript event.
type TranscriptEventType string

const (
	// TranscriptEventPartial indicates an interim/partial transcription result.
	TranscriptEventPartial TranscriptEventType = "partial"

	// TranscriptEventFinal indicates a final transcription segment.
	TranscriptEventFinal TranscriptEventType = "final"

	// TranscriptEventError indicates an error during transcription.
	TranscriptEventError TranscriptEventType = "error"
)

// Option is a functional option for configuring STT requests.
type Option func(*Options)

// Apply applies all options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithModel sets the STT model to use.
func WithModel(model string) Option {
	return func(o *Options) {
		o.Model = model
	}
}

// WithLanguage sets the language for transcription.
func WithLanguage(lang string) Option {
	return func(o *Options) {
		o.Language = lang
	}
}

// WithDiarization enables speaker diarization.
func WithDiarization(enabled bool) Option {
	return func(o *Options) {
		o.Diarize = enabled
	}
}

// WithTimestamps enables word-level timestamps.
func WithTimestamps(enabled bool) Option {
	return func(o *Options) {
		o.Timestamps = enabled
	}
}

// WithVocabulary sets custom vocabulary/keywords to improve recognition.
func WithVocabulary(words []string) Option {
	return func(o *Options) {
		o.Vocabulary = words
	}
}

// WithCustomOption sets a provider-specific option.
func WithCustomOption(key string, value any) Option {
	return func(o *Options) {
		if o.Custom == nil {
			o.Custom = make(map[string]any)
		}
		o.Custom[key] = value
	}
}

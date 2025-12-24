// Package tts provides Text-to-Speech provider interfaces and types.
package tts

import (
	"context"
	"time"
)

// Provider is the interface for text-to-speech providers.
type Provider interface {
	// Synthesize converts text to audio.
	Synthesize(ctx context.Context, text string, opts Options) (*Audio, error)

	// StreamSynthesize streams text and returns audio chunks as they're generated.
	// The text channel should be closed when all text has been sent.
	StreamSynthesize(ctx context.Context, text <-chan string, opts Options) (*AudioStream, error)

	// Voices returns available voices for this provider.
	Voices() []Voice

	// Capabilities returns the provider's capabilities.
	Capabilities() Capabilities
}

// Capabilities describes the features supported by a TTS provider.
type Capabilities struct {
	Provider  string   // Provider identifier (e.g., "elevenlabs")
	Voices    []Voice  // Available voices
	Languages []string // Supported language codes
	Realtime  bool     // Supports real-time streaming
	SSML      bool     // Supports SSML input
	Cloning   bool     // Supports voice cloning
}

// Options configures a synthesis request.
type Options struct {
	Voice      string         // Voice identifier (e.g., "rachel")
	Model      string         // Model identifier (for providers with multiple models)
	Speed      float64        // Speech speed multiplier (1.0 = normal, 0 uses default)
	Pitch      float64        // Pitch adjustment (-1.0 to 1.0, 0 = normal)
	Format     AudioFormat    // Output audio format
	SampleRate int            // Sample rate in Hz (0 uses default)
	Custom     map[string]any // Provider-specific options
}

// Voice represents an available voice.
type Voice struct {
	ID          string   // Unique voice identifier
	Name        string   // Display name
	Language    string   // Primary language code (e.g., "en-US")
	Gender      string   // "male", "female", or "neutral"
	Description string   // Voice description
	Preview     string   // URL to sample audio (optional)
	Styles      []string // Available speaking styles (optional)
}

// Audio represents synthesized audio.
type Audio struct {
	Data       []byte        // Audio bytes
	Format     AudioFormat   // Audio format
	SampleRate int           // Sample rate in Hz
	Duration   time.Duration // Audio duration
	Voice      string        // Voice used
	Model      string        // Model used
	Provider   string        // Provider used
}

// AudioFormat specifies the audio output format.
type AudioFormat string

const (
	FormatMP3  AudioFormat = "mp3"
	FormatWAV  AudioFormat = "wav"
	FormatPCM  AudioFormat = "pcm"
	FormatOGG  AudioFormat = "ogg"
	FormatFLAC AudioFormat = "flac"
	FormatOpus AudioFormat = "opus"
	FormatAAC  AudioFormat = "aac"
)

// AudioStream represents streaming audio output.
type AudioStream struct {
	events chan AudioEvent
	err    error
	done   chan struct{}
	meta   *AudioMeta
}

// AudioMeta contains metadata about the audio stream.
type AudioMeta struct {
	Format     AudioFormat
	SampleRate int
	Voice      string
	Model      string
	Provider   string
}

// NewAudioStream creates a new audio stream.
func NewAudioStream(meta *AudioMeta) *AudioStream {
	return &AudioStream{
		events: make(chan AudioEvent, 100),
		done:   make(chan struct{}),
		meta:   meta,
	}
}

// Events returns the channel of audio events.
func (s *AudioStream) Events() <-chan AudioEvent {
	return s.events
}

// Send sends an event to the stream. Returns false if stream is closed.
func (s *AudioStream) Send(event AudioEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.done:
		return false
	}
}

// Close closes the stream.
func (s *AudioStream) Close() error {
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
func (s *AudioStream) Err() error {
	return s.err
}

// SetError sets the stream error.
func (s *AudioStream) SetError(err error) {
	s.err = err
}

// Meta returns metadata about the audio stream.
func (s *AudioStream) Meta() *AudioMeta {
	return s.meta
}

// Collect collects all audio chunks into a single Audio result.
// This consumes the stream and blocks until completion.
func (s *AudioStream) Collect() (*Audio, error) {
	var data []byte
	for event := range s.events {
		switch event.Type {
		case AudioEventDelta:
			data = append(data, event.Data...)
		case AudioEventError:
			return nil, event.Error
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &Audio{
		Data:       data,
		Format:     s.meta.Format,
		SampleRate: s.meta.SampleRate,
		Voice:      s.meta.Voice,
		Model:      s.meta.Model,
		Provider:   s.meta.Provider,
	}, nil
}

// AudioEvent represents a streaming audio event.
type AudioEvent struct {
	Type      AudioEventType // Event type
	Data      []byte         // Audio chunk (for AudioEventDelta)
	Error     error          // Error (for AudioEventError)
	Timestamp time.Time      // When this event was generated
}

// AudioEventType identifies the type of audio event.
type AudioEventType string

const (
	// AudioEventDelta indicates an audio chunk.
	AudioEventDelta AudioEventType = "delta"

	// AudioEventFinish indicates the stream has finished.
	AudioEventFinish AudioEventType = "finish"

	// AudioEventError indicates an error during synthesis.
	AudioEventError AudioEventType = "error"
)

// Option is a functional option for configuring TTS requests.
type Option func(*Options)

// Apply applies all options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithVoice sets the voice to use for synthesis.
func WithVoice(voice string) Option {
	return func(o *Options) {
		o.Voice = voice
	}
}

// WithModel sets the TTS model to use.
func WithModel(model string) Option {
	return func(o *Options) {
		o.Model = model
	}
}

// WithSpeed sets the speech speed multiplier.
func WithSpeed(speed float64) Option {
	return func(o *Options) {
		o.Speed = speed
	}
}

// WithPitch sets the pitch adjustment.
func WithPitch(pitch float64) Option {
	return func(o *Options) {
		o.Pitch = pitch
	}
}

// WithFormat sets the output audio format.
func WithFormat(format AudioFormat) Option {
	return func(o *Options) {
		o.Format = format
	}
}

// WithSampleRate sets the output sample rate in Hz.
func WithSampleRate(rate int) Option {
	return func(o *Options) {
		o.SampleRate = rate
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

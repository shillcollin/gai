package testutil

import (
	"context"
	"time"

	"github.com/shillcollin/gai/tts"
)

// MockTTSProvider is a mock TTS provider for testing.
type MockTTSProvider struct {
	// SynthesizeFunc allows customizing the synthesize behavior.
	SynthesizeFunc func(ctx context.Context, text string, opts tts.Options) (*tts.Audio, error)

	// StreamSynthesizeFunc allows customizing the stream synthesize behavior.
	StreamSynthesizeFunc func(ctx context.Context, text <-chan string, opts tts.Options) (*tts.AudioStream, error)

	// Default audio format for simple tests
	DefaultFormat     tts.AudioFormat
	DefaultSampleRate int

	// Track calls for assertions
	SynthesizeCalls []SynthesizeCall
}

// SynthesizeCall records a call to Synthesize.
type SynthesizeCall struct {
	Text string
	Opts tts.Options
}

// NewMockTTS creates a new mock TTS provider.
func NewMockTTS() *MockTTSProvider {
	return &MockTTSProvider{
		DefaultFormat:     tts.FormatMP3,
		DefaultSampleRate: 24000,
	}
}

// Synthesize implements tts.Provider.
func (m *MockTTSProvider) Synthesize(ctx context.Context, text string, opts tts.Options) (*tts.Audio, error) {
	m.SynthesizeCalls = append(m.SynthesizeCalls, SynthesizeCall{Text: text, Opts: opts})

	if m.SynthesizeFunc != nil {
		return m.SynthesizeFunc(ctx, text, opts)
	}

	// Generate fake audio data (1 byte per character, scaled up)
	audioData := make([]byte, len(text)*100)
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	format := m.DefaultFormat
	if opts.Format != "" {
		format = opts.Format
	}

	sampleRate := m.DefaultSampleRate
	if opts.SampleRate > 0 {
		sampleRate = opts.SampleRate
	}

	voice := opts.Voice
	if voice == "" {
		voice = "default"
	}

	return &tts.Audio{
		Data:       audioData,
		Format:     format,
		SampleRate: sampleRate,
		Duration:   time.Duration(len(text)) * 50 * time.Millisecond, // ~50ms per character
		Voice:      voice,
		Model:      opts.Model,
		Provider:   "mock",
	}, nil
}

// StreamSynthesize implements tts.Provider.
func (m *MockTTSProvider) StreamSynthesize(ctx context.Context, textChan <-chan string, opts tts.Options) (*tts.AudioStream, error) {
	if m.StreamSynthesizeFunc != nil {
		return m.StreamSynthesizeFunc(ctx, textChan, opts)
	}

	format := m.DefaultFormat
	if opts.Format != "" {
		format = opts.Format
	}

	stream := tts.NewAudioStream(&tts.AudioMeta{
		Format:     format,
		SampleRate: m.DefaultSampleRate,
		Voice:      opts.Voice,
		Model:      opts.Model,
		Provider:   "mock",
	})

	go func() {
		defer stream.Close()

		for text := range textChan {
			select {
			case <-ctx.Done():
				return
			default:
				// Generate fake audio chunk for each text chunk
				audioData := make([]byte, len(text)*100)
				for i := range audioData {
					audioData[i] = byte(i % 256)
				}

				stream.Send(tts.AudioEvent{
					Type:      tts.AudioEventDelta,
					Data:      audioData,
					Timestamp: time.Now(),
				})
			}
		}

		// Send finish event
		stream.Send(tts.AudioEvent{
			Type:      tts.AudioEventFinish,
			Timestamp: time.Now(),
		})
	}()

	return stream, nil
}

// Voices implements tts.Provider.
func (m *MockTTSProvider) Voices() []tts.Voice {
	return []tts.Voice{
		{
			ID:          "default",
			Name:        "Default Voice",
			Language:    "en",
			Gender:      "neutral",
			Description: "Default mock voice",
		},
		{
			ID:          "rachel",
			Name:        "Rachel",
			Language:    "en",
			Gender:      "female",
			Description: "Mock female voice",
		},
		{
			ID:          "adam",
			Name:        "Adam",
			Language:    "en",
			Gender:      "male",
			Description: "Mock male voice",
		},
	}
}

// Capabilities implements tts.Provider.
func (m *MockTTSProvider) Capabilities() tts.Capabilities {
	return tts.Capabilities{
		Provider:  "mock",
		Voices:    m.Voices(),
		Languages: []string{"en", "es", "fr", "de"},
		Realtime:  true,
		SSML:      true,
		Cloning:   false,
	}
}

// MockTTSWithDelay creates a mock that simulates processing delay.
func MockTTSWithDelay(delay time.Duration) *MockTTSProvider {
	return &MockTTSProvider{
		DefaultFormat:     tts.FormatMP3,
		DefaultSampleRate: 24000,
		SynthesizeFunc: func(ctx context.Context, text string, opts tts.Options) (*tts.Audio, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				audioData := make([]byte, len(text)*100)
				return &tts.Audio{
					Data:       audioData,
					Format:     tts.FormatMP3,
					SampleRate: 24000,
					Duration:   time.Duration(len(text)) * 50 * time.Millisecond,
					Voice:      opts.Voice,
					Provider:   "mock",
				}, nil
			}
		},
	}
}

// MockTTSWithError creates a mock that returns an error.
func MockTTSWithError(err error) *MockTTSProvider {
	return &MockTTSProvider{
		SynthesizeFunc: func(ctx context.Context, text string, opts tts.Options) (*tts.Audio, error) {
			return nil, err
		},
	}
}

package testutil

import (
	"context"
	"io"
	"time"

	"github.com/shillcollin/gai/stt"
)

// MockSTTProvider is a mock STT provider for testing.
type MockSTTProvider struct {
	// TranscribeFunc allows customizing the transcribe behavior.
	TranscribeFunc func(ctx context.Context, audio []byte, opts stt.Options) (*stt.Transcript, error)

	// StreamTranscribeFunc allows customizing the stream transcribe behavior.
	StreamTranscribeFunc func(ctx context.Context, audio io.Reader, opts stt.Options) (*stt.TranscriptStream, error)

	// Default response for simple tests
	DefaultText     string
	DefaultLanguage string

	// Track calls for assertions
	TranscribeCalls []TranscribeCall
}

// TranscribeCall records a call to Transcribe.
type TranscribeCall struct {
	Audio []byte
	Opts  stt.Options
}

// NewMockSTT creates a new mock STT provider.
func NewMockSTT() *MockSTTProvider {
	return &MockSTTProvider{
		DefaultText:     "Hello, this is transcribed text.",
		DefaultLanguage: "en",
	}
}

// Transcribe implements stt.Provider.
func (m *MockSTTProvider) Transcribe(ctx context.Context, audio []byte, opts stt.Options) (*stt.Transcript, error) {
	m.TranscribeCalls = append(m.TranscribeCalls, TranscribeCall{Audio: audio, Opts: opts})

	if m.TranscribeFunc != nil {
		return m.TranscribeFunc(ctx, audio, opts)
	}

	return &stt.Transcript{
		Text:       m.DefaultText,
		Language:   m.DefaultLanguage,
		Confidence: 0.95,
		Duration:   float64(len(audio)) / 16000.0, // Assume 16kHz audio
		Model:      opts.Model,
		Provider:   "mock",
	}, nil
}

// StreamTranscribe implements stt.Provider.
func (m *MockSTTProvider) StreamTranscribe(ctx context.Context, audio io.Reader, opts stt.Options) (*stt.TranscriptStream, error) {
	if m.StreamTranscribeFunc != nil {
		return m.StreamTranscribeFunc(ctx, audio, opts)
	}

	// Return a simple stream that emits one transcript
	stream := stt.NewTranscriptStream()

	go func() {
		defer stream.Close()
		stream.Send(stt.TranscriptEvent{
			Type:       stt.TranscriptEventFinal,
			Final:      m.DefaultText,
			IsFinal:    true,
			Confidence: 0.95,
			Timestamp:  time.Now(),
		})
	}()

	return stream, nil
}

// Capabilities implements stt.Provider.
func (m *MockSTTProvider) Capabilities() stt.Capabilities {
	return stt.Capabilities{
		Provider:    "mock",
		Models:      []string{"default"},
		Languages:   []string{"en", "es", "fr", "de"},
		Realtime:    true,
		Diarization: true,
		Timestamps:  true,
		MaxDuration: 3600,
	}
}

// MockSTTWithDelay creates a mock that simulates processing delay.
func MockSTTWithDelay(delay time.Duration, text string) *MockSTTProvider {
	return &MockSTTProvider{
		TranscribeFunc: func(ctx context.Context, audio []byte, opts stt.Options) (*stt.Transcript, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				return &stt.Transcript{
					Text:       text,
					Language:   "en",
					Confidence: 0.95,
					Provider:   "mock",
				}, nil
			}
		},
	}
}

// MockSTTWithError creates a mock that returns an error.
func MockSTTWithError(err error) *MockSTTProvider {
	return &MockSTTProvider{
		TranscribeFunc: func(ctx context.Context, audio []byte, opts stt.Options) (*stt.Transcript, error) {
			return nil, err
		},
	}
}

package gai

import (
	"github.com/shillcollin/gai/core"
)

// Stream wraps core.Stream with the same interface.
// It provides access to streaming events from generation.
type Stream struct {
	inner *core.Stream
}

// newStream wraps a core.Stream.
func newStream(s *core.Stream) *Stream {
	return &Stream{inner: s}
}

// Events returns the channel of streaming events.
// Consume this channel to receive events as they arrive.
func (s *Stream) Events() <-chan core.StreamEvent {
	if s.inner == nil {
		return nil
	}
	return s.inner.Events()
}

// Err returns the terminal error if the stream failed.
func (s *Stream) Err() error {
	if s.inner == nil {
		return nil
	}
	return s.inner.Err()
}

// Close closes the stream and releases resources.
func (s *Stream) Close() error {
	if s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// Wait blocks until the stream is closed and returns the terminal error.
func (s *Stream) Wait() error {
	if s.inner == nil {
		return nil
	}
	return s.inner.Wait()
}

// Meta returns metadata about the stream (model, provider, usage).
func (s *Stream) Meta() core.StreamMeta {
	if s.inner == nil {
		return core.StreamMeta{}
	}
	return s.inner.Meta()
}

// Warnings returns any warnings accumulated during streaming.
func (s *Stream) Warnings() []core.Warning {
	if s.inner == nil {
		return nil
	}
	return s.inner.Warnings()
}

// Core returns the underlying core.Stream for advanced use cases.
func (s *Stream) Core() *core.Stream {
	return s.inner
}

// CollectText is a convenience method that consumes all events and
// returns the complete text. It closes the stream when done.
func (s *Stream) CollectText() (string, error) {
	if s.inner == nil {
		return "", nil
	}
	defer s.Close()

	var text string
	for event := range s.Events() {
		switch event.Type {
		case core.EventTextDelta:
			text += event.TextDelta
		case core.EventError:
			if event.Error != nil {
				return text, event.Error
			}
		}
	}

	if err := s.Err(); err != nil && err != core.ErrStreamClosed {
		return text, err
	}

	return text, nil
}

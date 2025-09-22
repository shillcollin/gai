package stream

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/core"
)

const defaultSSEBuffer = 16 * 1024

// SSE streams a core.Stream to an HTTP response writer using Server-Sent Events.
func SSE(w http.ResponseWriter, s *core.Stream) error {
	return SSEWithPolicy(w, s, Policy{})
}

// SSEWithPolicy streams events honouring the provided Policy.
func SSEWithPolicy(w http.ResponseWriter, s *core.Stream, policy Policy) error {
	if w != nil {
		headers := w.Header()
		headers.Set("Content-Type", "text/event-stream")
		headers.Set("Cache-Control", "no-cache")
		headers.Set("X-Accel-Buffering", "no")
	}

	bufferSize := policy.BufferSize
	if bufferSize <= 0 {
		bufferSize = defaultSSEBuffer
	}

	writer := newSSEWriter(w, bufferSize)
	defer writer.Close()

	for event := range s.Events() {
		filtered, ok := filterEvent(policy, event)
		if !ok {
			continue
		}
		if policy.MaskErrors && filtered.Type == core.EventError {
			filtered = maskError(filtered)
		}
		if err := writer.Write(filtered); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return s.Err()
}

// SSEWriter emits gai StreamEvents using text/event-stream.
type SSEWriter struct {
	mu     sync.Mutex
	writer *bufio.Writer
	w      http.ResponseWriter
}

func newSSEWriter(w http.ResponseWriter, bufferSize int) *SSEWriter {
	if bufferSize <= 0 {
		bufferSize = defaultSSEBuffer
	}
	return &SSEWriter{
		writer: bufio.NewWriterSize(w, bufferSize),
		w:      w,
	}
}

// NewSSEWriter constructs a writer for the provided response writer.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	return newSSEWriter(w, defaultSSEBuffer)
}

// Write encodes and emits a single event.
func (s *SSEWriter) Write(event core.StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	if _, err := s.writer.WriteString("data: "); err != nil {
		return err
	}
	if _, err := s.writer.Write(payload); err != nil {
		return err
	}
	if _, err := s.writer.WriteString("\n\n"); err != nil {
		return err
	}
	return nil
}

// Flush flushes the buffered writer and underlying ResponseWriter if possible.
func (s *SSEWriter) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writer.Flush(); err != nil {
		return err
	}
	if flusher, ok := s.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// Close flushes buffers. It is safe to call multiple times.
func (s *SSEWriter) Close() error {
	if s == nil {
		return nil
	}
	if err := s.Flush(); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
		return err
	}
	return nil
}

package stream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/core"
)

// SSE streams a core.Stream to an HTTP response writer using Server-Sent Events.
func SSE(w http.ResponseWriter, s *core.Stream) error {
	writer := NewSSEWriter(w)
	defer writer.Close()

	for event := range s.Events() {
		if err := writer.Write(event); err != nil {
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

// NewSSEWriter constructs a writer for the provided response writer.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	return &SSEWriter{
		writer: bufio.NewWriterSize(w, 16*1024),
		w:      w,
	}
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
	return s.Flush()
}

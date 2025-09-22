package stream

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/shillcollin/gai/core"
)

// NDJSON streams events as newline-delimited JSON.
func NDJSON(w http.ResponseWriter, s *core.Stream) error {
	if w != nil {
		headers := w.Header()
		headers.Set("Content-Type", "application/x-ndjson")
		headers.Set("Cache-Control", "no-cache")
	}
	writer := NewNDJSONWriter(w)
	for event := range s.Events() {
		if err := writer.Write(event); err != nil {
			return err
		}
	}
	return s.Err()
}

// NDJSONWriter serialises events to newline-delimited JSON.
type NDJSONWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	w       http.ResponseWriter
}

// NewNDJSONWriter builds a writer for NDJSON payloads.
func NewNDJSONWriter(w http.ResponseWriter) *NDJSONWriter {
	return &NDJSONWriter{
		encoder: json.NewEncoder(w),
		w:       w,
	}
}

// Write emits the provided event as a JSON line.
func (w *NDJSONWriter) Write(event core.StreamEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.encoder.Encode(event); err != nil {
		return err
	}
	if flusher, ok := w.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

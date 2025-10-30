package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileEmitter appends AgentEventV1 frames to progress/events.ndjson.
type FileEmitter struct {
	path string
	mu   sync.Mutex
}

// NewFileEmitter creates an emitter rooted at progressDir.
func NewFileEmitter(progressDir string) (*FileEmitter, error) {
	if err := os.MkdirAll(progressDir, 0o755); err != nil {
		return nil, fmt.Errorf("events: create progress dir: %w", err)
	}
	return &FileEmitter{path: filepath.Join(progressDir, "events.ndjson")}, nil
}

// Emit writes the event to disk after validation.
func (e *FileEmitter) Emit(event AgentEventV1) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if event.Ts == 0 {
		event.Ts = Now()
	}
	if event.Version == "" {
		event.Version = SchemaVersionV1
	}
	if err := event.Validate(); err != nil {
		return err
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: marshal event: %w", err)
	}

	f, err := os.OpenFile(e.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("events: open file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(payload); err != nil {
		return fmt.Errorf("events: write payload: %w", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("events: write newline: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("events: sync: %w", err)
	}
	return nil
}

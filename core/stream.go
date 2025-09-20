package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrStreamClosed indicates the stream has already been closed.
var ErrStreamClosed = errors.New("stream closed")

// ErrStreamFinalized indicates the stream has already been finalized.
var ErrStreamFinalized = errors.New("stream already finalized")

// EventType enumerates stream event types.
type EventType string

const (
	EventStart            EventType = "start"
	EventTextDelta        EventType = "text.delta"
	EventReasoningDelta   EventType = "reasoning.delta"
	EventReasoningSummary EventType = "reasoning.summary"
	EventAudioDelta       EventType = "audio.delta"
	EventToolCall         EventType = "tool.call"
	EventToolResult       EventType = "tool.result"
	EventCitations        EventType = "citations"
	EventSafety           EventType = "safety"
	EventStepStart        EventType = "step.start"
	EventStepFinish       EventType = "step.finish"
	EventFinish           EventType = "finish"
	EventError            EventType = "error"
)

// StreamEvent models a single event within the normalized stream.
type StreamEvent struct {
	Type      EventType `json:"type"`
	Schema    string    `json:"schema"`
	Seq       int       `json:"seq"`
	Timestamp time.Time `json:"ts"`
	StreamID  string    `json:"stream_id"`
	RequestID string    `json:"request_id,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	TraceID   string    `json:"trace_id,omitempty"`
	SpanID    string    `json:"span_id,omitempty"`

	TextDelta        string         `json:"text,omitempty"`
	ReasoningDelta   string         `json:"reasoning,omitempty"`
	ReasoningSummary string         `json:"reasoning_summary,omitempty"`
	ToolCall         ToolCall       `json:"tool_call,omitempty"`
	ToolResult       ToolResult     `json:"tool_result,omitempty"`
	Citations        []Citation     `json:"citations,omitempty"`
	Safety           *SafetyEvent   `json:"safety,omitempty"`
	Usage            Usage          `json:"usage,omitempty"`
	FinishReason     *StopReason    `json:"finish_reason,omitempty"`
	Error            error          `json:"-"`
	StepID           int            `json:"step_id,omitempty"`
	DurationMS       int64          `json:"duration_ms,omitempty"`
	ToolCalls        int            `json:"tool_calls,omitempty"`
	Capabilities     []string       `json:"capabilities,omitempty"`
	Policies         map[string]any `json:"policies,omitempty"`
	Ext              map[string]any `json:"ext,omitempty"`
}

// StreamMeta captures final metadata emitted on finish events.
type StreamMeta struct {
	Model    string
	Provider string
	Usage    Usage
}

// Stream represents a streaming response from a provider.
type Stream struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.RWMutex
	events chan StreamEvent
	err    error
	closed bool
	meta   StreamMeta
}

// NewStream constructs a Stream with the provided event buffer size.
func NewStream(ctx context.Context, buffer int) *Stream {
	if buffer <= 0 {
		buffer = 16
	}
	c, cancel := context.WithCancel(ctx)
	return &Stream{
		ctx:    c,
		cancel: cancel,
		events: make(chan StreamEvent, buffer),
	}
}

// Push appends a new event to the stream. It is safe to call from multiple goroutines.
func (s *Stream) Push(event StreamEvent) {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return
	}

	if event.Type == EventFinish {
		s.mu.Lock()
		s.meta = StreamMeta{
			Model:    event.Model,
			Provider: event.Provider,
			Usage:    event.Usage,
		}
		s.mu.Unlock()
	}

	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

// Close closes the stream channel and cancels context.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStreamClosed
	}
	s.closed = true
	close(s.events)
	s.cancel()
	return nil
}

// Events returns a read-only channel of events.
func (s *Stream) Events() <-chan StreamEvent {
	return s.events
}

// Err returns the terminal error, if any.
func (s *Stream) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// Meta returns metadata associated with the stream.
func (s *Stream) Meta() StreamMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta
}

// Wait blocks until the stream is closed and returns the terminal error.
func (s *Stream) Wait() error {
	<-s.ctx.Done()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// Fail marks the stream as failed and closes it.
func (s *Stream) Fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	alreadyClosed := s.closed
	s.mu.Unlock()

	if err != nil {
		s.Push(StreamEvent{Type: EventError, Error: err})
	}
	if !alreadyClosed {
		_ = s.Close()
	}
}

// SetMeta updates the stream metadata; used when finish event lacks ext fields.
func (s *Stream) SetMeta(meta StreamMeta) {
	s.mu.Lock()
	s.meta = meta
	s.mu.Unlock()
}

// ExtValue fetches a convenience value from ext map if present.
func (e StreamEvent) ExtValue(key string) string {
	if e.Ext == nil {
		return ""
	}
	if v, ok := e.Ext[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

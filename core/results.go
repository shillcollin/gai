package core

import (
	"errors"
)

// Usage captures token accounting and costs returned by providers.
type Usage struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens,omitempty"`
	TotalTokens     int     `json:"total_tokens"`
	CostUSD         float64 `json:"cost_usd,omitempty"`

	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	AudioTokens       int `json:"audio_tokens,omitempty"`
}

// StopReason documents why generation ended.
type StopReason struct {
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
}

// Citation points to a supporting source reference.
type Citation struct {
	URI     string  `json:"uri"`
	Title   string  `json:"title,omitempty"`
	Snippet string  `json:"snippet,omitempty"`
	Start   int     `json:"start,omitempty"`
	End     int     `json:"end,omitempty"`
	Score   float32 `json:"score,omitempty"`
}

// TextResult represents a non-streaming text generation response.
type TextResult struct {
	Text         string        `json:"text"`
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	Usage        Usage         `json:"usage"`
	Steps        []Step        `json:"steps,omitempty"`
	Citations    []Citation    `json:"citations,omitempty"`
	Safety       []SafetyEvent `json:"safety,omitempty"`
	FinishReason StopReason    `json:"finish_reason"`
	LatencyMS    int64         `json:"latency_ms,omitempty"`
	TTFBMS       int64         `json:"ttfb_ms,omitempty"`
}

// ObjectResultRaw contains structured JSON output as raw bytes.
type ObjectResultRaw struct {
	JSON     []byte `json:"json"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Usage    Usage  `json:"usage"`
}

// ObjectResult provides a typed representation of structured output.
type ObjectResult[T any] struct {
	Value    T
	RawJSON  []byte
	Model    string
	Provider string
	Usage    Usage
}

// ObjectStreamRaw wraps streaming structured JSON events.
type ObjectStreamRaw struct {
	stream *Stream
}

// NewObjectStreamRaw constructs an ObjectStreamRaw from a stream.
func NewObjectStreamRaw(stream *Stream) *ObjectStreamRaw {
	return &ObjectStreamRaw{stream: stream}
}

// Stream returns the underlying stream instance.
func (s *ObjectStreamRaw) Stream() *Stream {
	if s == nil {
		return nil
	}
	return s.stream
}

// ObjectStream wraps a streaming structured JSON pipeline decoding into T.
type ObjectStream[T any] struct {
	stream    *Stream
	decoder   StructuredDecoder[T]
	finalized bool
}

// StructuredDecoder exposes incremental decoding for streaming JSON.
type StructuredDecoder[T any] interface {
	Feed(chunk []byte) error
	Finalize() (T, error)
}

// Events exposes the underlying stream events.
func (s *ObjectStream[T]) Events() <-chan StreamEvent {
	if s == nil || s.stream == nil {
		return nil
	}
	return s.stream.Events()
}

// Close closes the underlying stream.
func (s *ObjectStream[T]) Close() error {
	if s == nil || s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

// Final returns the final decoded value, consuming any buffered data.
func (s *ObjectStream[T]) Final() (*ObjectResult[T], error) {
	if s == nil {
		return nil, ErrStreamClosed
	}
	if s.finalized {
		return nil, ErrStreamFinalized
	}
	defer func() { s.finalized = true }()

	if err := s.stream.Wait(); err != nil && !errors.Is(err, ErrStreamClosed) {
		return nil, err
	}
	value, err := s.decoder.Finalize()
	if err != nil {
		return nil, err
	}
	res := &ObjectResult[T]{
		Value:    value,
		Model:    s.stream.meta.Model,
		Provider: s.stream.meta.Provider,
		Usage:    s.stream.meta.Usage,
	}
	return res, nil
}

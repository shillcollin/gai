package stream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shillcollin/gai/core"
	internalstream "github.com/shillcollin/gai/internal/stream"
)

// Format identifies the upstream stream encoding.
type Format int

const (
	FormatSSE Format = iota
	FormatNDJSON
)

// Reader consumes SSE or NDJSON streams and emits normalised StreamEvents.
type Reader struct {
	source    io.ReadCloser
	format    Format
	decoder   *json.Decoder
	scanner   *bufio.Reader
	validator *internalstream.Validator
	done      bool
}

// NewReader constructs a reader for the given format.
func NewReader(r io.ReadCloser, format Format) *Reader {
	reader := &Reader{
		source:    r,
		format:    format,
		validator: &internalstream.Validator{},
	}
	if format == FormatNDJSON {
		reader.decoder = json.NewDecoder(r)
	} else {
		reader.scanner = bufio.NewReader(r)
	}
	return reader
}

// Read returns the next normalised event.
func (r *Reader) Read() (core.StreamEvent, error) {
	if r.done {
		return core.StreamEvent{}, io.EOF
	}
	raw, err := r.nextRaw()
	if err != nil {
		if errors.Is(err, io.EOF) {
			r.done = true
		}
		return core.StreamEvent{}, err
	}
	if err := internalstream.ValidateRaw(raw); err != nil {
		return core.StreamEvent{}, err
	}
	if r.validator != nil {
		if err := r.validator.Validate(raw); err != nil {
			return core.StreamEvent{}, err
		}
	}
	var event core.StreamEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return core.StreamEvent{}, fmt.Errorf("decode stream event: %w", err)
	}
	return event, nil
}

func (r *Reader) nextRaw() ([]byte, error) {
	switch r.format {
	case FormatNDJSON:
		var raw json.RawMessage
		if err := r.decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				if r.validator != nil {
					if err := r.validator.EnsureFinished(); err != nil {
						return nil, err
					}
				}
				return nil, io.EOF
			}
			return nil, err
		}
		return raw, nil
	case FormatSSE:
		return r.readSSE()
	default:
		return nil, fmt.Errorf("unsupported format %d", r.format)
	}
}

func (r *Reader) readSSE() ([]byte, error) {
	var data bytes.Buffer
	for {
		line, err := r.scanner.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && data.Len() == 0 {
				if r.validator != nil {
					if err := r.validator.EnsureFinished(); err != nil {
						return nil, err
					}
				}
				return nil, io.EOF
			}
			if errors.Is(err, io.EOF) {
				// Treat unterminated event as error to surface truncated payloads.
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			return data.Bytes(), nil
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(payload)
		}
	}
}

// Close releases the underlying reader.
func (r *Reader) Close() error {
	r.done = true
	if r.source != nil {
		return r.source.Close()
	}
	return nil
}

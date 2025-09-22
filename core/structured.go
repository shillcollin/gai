package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shillcollin/gai/internal/jsonschema"
)

// StrictMode controls structured output repair behaviour.
type StrictMode int

const (
	StrictProvider StrictMode = iota
	StrictRepair
	StrictBoth
)

// StructuredOptions customise typed structured output helpers.
type StructuredOptions struct {
	Mode StrictMode
}

// GenerateObjectTyped generates structured output and unmarshals into T with validation.
func GenerateObjectTyped[T any](ctx context.Context, p Provider, req Request, opts ...StructuredOptions) (*ObjectResult[T], error) {
	var options StructuredOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	raw, err := p.GenerateObject(ctx, req)
	if err != nil {
		return nil, err
	}

	decoded, rawJSON, err := decodeStructured[T](raw.JSON, options)
	if err != nil {
		return nil, err
	}

	return &ObjectResult[T]{
		Value:    decoded,
		RawJSON:  rawJSON,
		Model:    raw.Model,
		Provider: raw.Provider,
		Usage:    raw.Usage,
		Warnings: append([]Warning(nil), raw.Warnings...),
	}, nil
}

// StreamObjectTyped streams structured output, decoding into T when the stream completes.
func StreamObjectTyped[T any](ctx context.Context, p Provider, req Request, opts ...StructuredOptions) (*ObjectStream[T], error) {
	var options StructuredOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	raw, err := p.StreamObject(ctx, req)
	if err != nil {
		return nil, err
	}
	underlying := raw.Stream()
	if underlying == nil {
		return nil, errors.New("stream object: provider returned nil stream")
	}
	decoder, err := newJSONStreamDecoder[T](options)
	if err != nil {
		return nil, err
	}
	proxy := NewStream(ctx, 64)
	go proxyStructuredStream(underlying, proxy, decoder)
	return &ObjectStream[T]{
		stream:  proxy,
		decoder: decoder,
	}, nil
}

// decodeStructured handles repair, validation and decoding into T.
func decodeStructured[T any](data []byte, options StructuredOptions) (T, []byte, error) {
	var out T
	validator, err := makeValidator[T]()
	if err != nil {
		var zero T
		return zero, nil, err
	}

	attempt := func(payload []byte) error {
		if validator != nil {
			if err := validator.Validate(payload); err != nil {
				return err
			}
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			return err
		}
		return nil
	}

	if err := attempt(data); err != nil {
		switch options.Mode {
		case StrictRepair, StrictBoth:
			repaired, repairErr := jsonschema.RepairJSON(data)
			if repairErr != nil {
				return out, nil, fmt.Errorf("repair json: %w", repairErr)
			}
			if err2 := attempt(repaired); err2 != nil {
				return out, nil, fmt.Errorf("decode repaired json: %w", err2)
			}
			data = repaired
		default:
			return out, nil, fmt.Errorf("decode structured output: %w", err)
		}
	}
	// Re-encode to canonical JSON for RawJSON.
	normalized, err := json.Marshal(out)
	if err != nil {
		return out, nil, fmt.Errorf("re-encode structured output: %w", err)
	}
	return out, normalized, nil
}

func makeValidator[T any]() (*jsonschema.Validator, error) {
	schemaDoc, err := jsonschema.Derive[T]()
	if err != nil {
		return nil, err
	}
	if schemaDoc == nil {
		return nil, nil
	}
	return jsonschema.Compile(schemaDoc)
}

type jsonStreamDecoder[T any] struct {
	options StructuredOptions
	buffer  bytes.Buffer
	rawJSON []byte
}

func newJSONStreamDecoder[T any](options StructuredOptions) (*jsonStreamDecoder[T], error) {
	return &jsonStreamDecoder[T]{options: options}, nil
}

func (d *jsonStreamDecoder[T]) Feed(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	_, err := d.buffer.Write(chunk)
	return err
}

func (d *jsonStreamDecoder[T]) Finalize() (T, error) {
	data := bytes.TrimSpace(d.buffer.Bytes())
	if len(data) == 0 {
		var zero T
		return zero, errors.New("structured stream produced no data")
	}
	value, rawJSON, err := decodeStructured[T](data, d.options)
	if err != nil {
		var zero T
		return zero, err
	}
	d.rawJSON = rawJSON
	return value, nil
}

func (d *jsonStreamDecoder[T]) RawJSON() []byte {
	if len(d.rawJSON) == 0 {
		return nil
	}
	clone := make([]byte, len(d.rawJSON))
	copy(clone, d.rawJSON)
	return clone
}

func proxyStructuredStream[T any](src *Stream, dst *Stream, decoder StructuredDecoder[T]) {
	defer func() {
		// Ensure the destination is closed exactly once.
		_ = dst.Close()
	}()
	for event := range src.Events() {
		if err := feedStructuredDecoder(decoder, event); err != nil {
			src.Fail(err)
			dst.Fail(err)
			return
		}
		dst.Push(event)
	}
	meta := src.Meta()
	if warnings := src.Warnings(); len(warnings) > 0 {
		dst.AddWarnings(warnings...)
	}
	dst.SetMeta(meta)
	if err := src.Err(); err != nil && !errors.Is(err, ErrStreamClosed) {
		dst.Fail(err)
	}
}

func feedStructuredDecoder[T any](decoder StructuredDecoder[T], event StreamEvent) error {
	if decoder == nil {
		return nil
	}
	var chunk string
	switch {
	case event.TextDelta != "":
		chunk = event.TextDelta
	case event.Ext != nil:
		if raw, ok := event.Ext["json"]; ok {
			if s, ok := raw.(string); ok {
				chunk = s
			}
		}
	}
	if chunk == "" {
		return nil
	}
	return decoder.Feed([]byte(chunk))
}

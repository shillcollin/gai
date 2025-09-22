package core

import (
	"context"
	"encoding/json"
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

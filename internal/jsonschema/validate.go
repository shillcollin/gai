package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/shillcollin/gai/schema"
)

// Validator wraps a schema for validation.
type Validator struct {
	schema *schema.Schema
}

// Compile constructs a Validator for the provided schema.
func Compile(schemaDoc *schema.Schema) (*Validator, error) {
	if schemaDoc == nil {
		return nil, errors.New("nil schema")
	}
	return &Validator{schema: schemaDoc}, nil
}

// Validate ensures data matches the schema definition.
func (v *Validator) Validate(data []byte) error {
	if v == nil {
		return errors.New("validator nil")
	}
	var payload any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return validateValue(payload, v.schema)
}

func validateValue(value any, s *schema.Schema) error {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
		for _, key := range s.Required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("missing required field %s", key)
			}
		}
		for key, prop := range s.Properties {
			if val, ok := obj[key]; ok {
				if err := validateValue(val, prop); err != nil {
					return fmt.Errorf("field %s: %w", key, err)
				}
			}
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
		if s.Items != nil {
			for i, elem := range arr {
				if err := validateValue(elem, s.Items); err != nil {
					return fmt.Errorf("index %d: %w", i, err)
				}
			}
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		if s.MinLength != nil && len(str) < *s.MinLength {
			return fmt.Errorf("string shorter than %d", *s.MinLength)
		}
		if s.MaxLength != nil && len(str) > *s.MaxLength {
			return fmt.Errorf("string longer than %d", *s.MaxLength)
		}
		if len(s.Enum) > 0 {
			match := false
			for _, v := range s.Enum {
				if vs, ok := v.(string); ok && vs == str {
					match = true
					break
				}
			}
			if !match {
				return fmt.Errorf("value %q not in enum", str)
			}
		}
		if s.Pattern != "" {
			re, err := regexp.Compile(s.Pattern)
			if err != nil {
				return fmt.Errorf("compile pattern: %w", err)
			}
			if !re.MatchString(str) {
				return fmt.Errorf("value %q does not match pattern", str)
			}
		}
	case "number":
		num, ok := value.(json.Number)
		if !ok {
			// allow float64 from default decoder
			if f, ok := value.(float64); ok {
				num = json.Number(fmt.Sprintf("%f", f))
			} else {
				return fmt.Errorf("expected number, got %T", value)
			}
		}
		f, err := num.Float64()
		if err != nil {
			return fmt.Errorf("parse number: %w", err)
		}
		if s.Minimum != nil && f < *s.Minimum {
			return fmt.Errorf("number %.2f below minimum %.2f", f, *s.Minimum)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return fmt.Errorf("number %.2f above maximum %.2f", f, *s.Maximum)
		}
		if len(s.Enum) > 0 {
			match := false
			for _, v := range s.Enum {
				switch val := v.(type) {
				case float64:
					if val == f {
						match = true
					}
				case string:
					if vf, err := json.Number(val).Float64(); err == nil && vf == f {
						match = true
					}
				}
			}
			if !match {
				return fmt.Errorf("number %.2f not in enum", f)
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "":
		// schema with unspecified type accepts any
		return nil
	default:
		return fmt.Errorf("unsupported schema type %s", s.Type)
	}
	return nil
}

// RepairJSON performs limited repairs by removing trailing commas and validating.
func RepairJSON(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty json")
	}
	if json.Valid(trimmed) {
		return trimmed, nil
	}
	// remove trailing commas before object/array end
	re := regexp.MustCompile(`,\s*(\}|\])`)
	fixed := re.ReplaceAll(trimmed, []byte("$1"))
	if json.Valid(fixed) {
		return fixed, nil
	}
	return nil, errors.New("unable to repair json")
}

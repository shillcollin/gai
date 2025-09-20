package jsonschema

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/shillcollin/gai/schema"
)

// Derive derives a JSONSchema representation for the provided generic type.
func Derive[T any]() (*schema.Schema, error) {
	var zero T
	return deriveType(reflect.TypeOf(zero))
}

func deriveType(t reflect.Type) (*schema.Schema, error) {
	if t == nil {
		return &schema.Schema{}, nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.PkgPath() == "time" && t.Name() == "Time" {
		return &schema.Schema{Type: "string", Format: "date-time"}, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return &schema.Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return &schema.Schema{Type: "number"}, nil
	case reflect.String:
		return &schema.Schema{Type: "string"}, nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return &schema.Schema{Type: "string", Format: "byte"}, nil
		}
		itemSchema, err := deriveType(t.Elem())
		if err != nil {
			return nil, err
		}
		return &schema.Schema{Type: "array", Items: itemSchema}, nil
	case reflect.Map:
		return &schema.Schema{Type: "object"}, nil
	case reflect.Struct:
		props := map[string]*schema.Schema{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" { // unexported
				continue
			}
			jsonTag := field.Tag.Get("json")
			name, opts := parseTag(jsonTag)
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			schema, err := deriveType(field.Type)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			applyFieldTags(schema, field.Tag)
			props[name] = schema
			if !opts.contains("omitempty") {
				required = append(required, name)
			}
		}
		sort.Strings(required)
		return &schema.Schema{Type: "object", Properties: props, Required: required}, nil
	case reflect.Interface:
		return &schema.Schema{}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", t.Kind())
	}
}

type tagOptions []string

func parseTag(tag string) (string, tagOptions) {
	if tag == "" {
		return "", nil
	}
	parts := strings.Split(tag, ",")
	if len(parts) == 0 {
		return tag, nil
	}
	return parts[0], tagOptions(parts[1:])
}

func (o tagOptions) contains(opt string) bool {
	for _, v := range o {
		if v == opt {
			return true
		}
	}
	return false
}

func applyFieldTags(schema *schema.Schema, tag reflect.StructTag) {
	if schema == nil {
		return
	}
	if desc := tag.Get("description"); desc != "" {
		schema.Description = desc
	}
	if def := tag.Get("default"); def != "" {
		schema.Default = def
	}
	if enumTag := tag.Get("enum"); enumTag != "" {
		values := strings.Split(enumTag, ",")
		enum := make([]any, len(values))
		for i, v := range values {
			enum[i] = v
		}
		schema.Enum = enum
	}
	if min := tag.Get("minimum"); min != "" {
		if f, err := strconv.ParseFloat(min, 64); err == nil {
			schema.Minimum = &f
		}
	}
	if max := tag.Get("maximum"); max != "" {
		if f, err := strconv.ParseFloat(max, 64); err == nil {
			schema.Maximum = &f
		}
	}
	if minLen := tag.Get("minLength"); minLen != "" {
		if i, err := strconv.Atoi(minLen); err == nil {
			schema.MinLength = &i
		}
	}
	if maxLen := tag.Get("maxLength"); maxLen != "" {
		if i, err := strconv.Atoi(maxLen); err == nil {
			schema.MaxLength = &i
		}
	}
	if pattern := tag.Get("pattern"); pattern != "" {
		schema.Pattern = pattern
	}
	if format := tag.Get("format"); format != "" {
		schema.Format = format
	}
}

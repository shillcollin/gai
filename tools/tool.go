package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/shillcollin/gai/core"
	jsonschema "github.com/shillcollin/gai/internal/jsonschema"
	"github.com/shillcollin/gai/schema"
)

// ToolFunc is the handler invoked when the model calls the tool.
type ToolFunc[I any, O any] func(ctx context.Context, input I, meta core.ToolMeta) (O, error)

// Tool represents a typed tool definition.
type Tool[I any, O any] struct {
	name        string
	description string
	fn          ToolFunc[I, O]

	once         sync.Once
	inputSchema  *schema.Schema
	outputSchema *schema.Schema
}

// New constructs a typed tool with derived schemas.
func New[I any, O any](name, description string, fn ToolFunc[I, O]) *Tool[I, O] {
	return &Tool[I, O]{name: name, description: description, fn: fn}
}

// Name returns the tool name.
func (t *Tool[I, O]) Name() string { return t.name }

// Description returns the description.
func (t *Tool[I, O]) Description() string { return t.description }

// InputSchema returns the JSON schema for the input type.
func (t *Tool[I, O]) InputSchema() *schema.Schema {
	t.ensureSchemas()
	return t.inputSchema
}

// OutputSchema returns the JSON schema for the output type.
func (t *Tool[I, O]) OutputSchema() *schema.Schema {
	t.ensureSchemas()
	return t.outputSchema
}

// Execute runs the underlying function using the provided map input.
func (t *Tool[I, O]) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	var args I
	if err := mapToStruct(input, &args); err != nil {
		return nil, err
	}
	result, err := t.fn(ctx, args, meta)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (t *Tool[I, O]) ensureSchemas() {
	t.once.Do(func() {
		in, err := jsonschema.Derive[I]()
		if err != nil {
			panic(fmt.Sprintf("derive input schema: %v", err))
		}
		out, err := jsonschema.Derive[O]()
		if err != nil {
			panic(fmt.Sprintf("derive output schema: %v", err))
		}
		t.inputSchema = in
		t.outputSchema = out
	})
}

func mapToStruct[M any](data map[string]any, target *M) error {
	buf, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal tool input: %w", err)
	}
	if err := json.Unmarshal(buf, target); err != nil {
		return fmt.Errorf("unmarshal tool input: %w", err)
	}
	return nil
}

// ToCoreAdapter converts the tool to a core.ToolHandle.
type ToCoreAdapter[I any, O any] struct {
	tool *Tool[I, O]
}

// NewCoreAdapter converts a typed tool into a core.ToolHandle.
func NewCoreAdapter[I any, O any](tool *Tool[I, O]) core.ToolHandle {
	return &ToCoreAdapter[I, O]{tool: tool}
}

func (a *ToCoreAdapter[I, O]) Name() string { return a.tool.Name() }

func (a *ToCoreAdapter[I, O]) Description() string { return a.tool.Description() }

func (a *ToCoreAdapter[I, O]) InputSchema() *schema.Schema { return a.tool.InputSchema() }

func (a *ToCoreAdapter[I, O]) OutputSchema() *schema.Schema { return a.tool.OutputSchema() }

func (a *ToCoreAdapter[I, O]) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	return a.tool.Execute(ctx, input, meta)
}

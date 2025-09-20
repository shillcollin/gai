package tools

import (
	"context"
	"testing"

	"github.com/shillcollin/gai/core"
)

type sampleInput struct {
	Name string `json:"name"`
}

type sampleOutput struct {
	Greeting string `json:"greeting"`
}

func TestToolExecution(t *testing.T) {
	tool := New[sampleInput, sampleOutput]("greet", "Generate greeting", func(ctx context.Context, in sampleInput, meta core.ToolMeta) (sampleOutput, error) {
		return sampleOutput{Greeting: "Hello " + in.Name}, nil
	})

	adapter := NewCoreAdapter(tool)
	result, err := adapter.Execute(context.Background(), map[string]any{"name": "gai"}, core.ToolMeta{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := result.(sampleOutput)
	if out.Greeting != "Hello gai" {
		t.Fatalf("unexpected output: %s", out.Greeting)
	}
}

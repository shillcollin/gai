package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
)

type fakeProvider struct {
	calls int
}

func (f *fakeProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	return nil, nil
}

func (f *fakeProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	stream := core.NewStream(ctx, 8)
	go func() {
		defer stream.Close()
		f.calls++
		if f.calls == 1 {
			stream.Push(core.StreamEvent{Type: core.EventToolCall, ToolCall: core.ToolCall{ID: "call-1", Name: "greet", Input: map[string]any{"name": "gai"}}, Schema: "gai.events.v1"})
			stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", FinishReason: &core.StopReason{Type: "tool_calls"}})
		} else {
			stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: "Hello gai", Schema: "gai.events.v1"})
			stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", FinishReason: &core.StopReason{Type: "stop"}})
		}
	}()
	return stream, nil
}

func (f *fakeProvider) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	return nil, nil
}
func (f *fakeProvider) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	return nil, nil
}
func (f *fakeProvider) Capabilities() core.Capabilities {
	return core.Capabilities{Provider: "fake", Streaming: true}
}

func TestRunnerToolExecution(t *testing.T) {
	provider := &fakeProvider{}
	runner := New(provider, WithMaxParallel(2))

	type input struct {
		Name string `json:"name"`
	}
	type output struct {
		Greeting string `json:"greeting"`
	}
	tool := tools.New[input, output](
		"greet",
		"Generate greeting",
		func(ctx context.Context, in input, meta core.ToolMeta) (output, error) {
			return output{Greeting: "Hello " + in.Name}, nil
		},
	)

	res, err := runner.ExecuteRequest(context.Background(), core.Request{
		Messages:   []core.Message{core.UserMessage(core.TextPart("Say hi"))},
		Tools:      []core.ToolHandle{tools.NewCoreAdapter(tool)},
		ToolChoice: core.ToolChoiceAuto,
	})
	if err != nil {
		t.Fatalf("ExecuteRequest error: %v", err)
	}
	if res.Text != "Hello gai" {
		t.Fatalf("unexpected final text: %s", res.Text)
	}
}

func TestRunnerStreamRequest(t *testing.T) {
	provider := &fakeProvider{}
	runner := New(provider, WithMaxParallel(2))

	type input struct {
		Name string `json:"name"`
	}
	type output struct {
		Greeting string `json:"greeting"`
	}
	tool := tools.New[input, output](
		"greet",
		"Generate greeting",
		func(ctx context.Context, in input, meta core.ToolMeta) (output, error) {
			return output{Greeting: "Hello " + in.Name}, nil
		},
	)

	stream, err := runner.StreamRequest(context.Background(), core.Request{
		Messages:   []core.Message{core.UserMessage(core.TextPart("Say hi"))},
		Tools:      []core.ToolHandle{tools.NewCoreAdapter(tool)},
		ToolChoice: core.ToolChoiceAuto,
	})
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}

	var (
		stepStart  bool
		stepFinish bool
		toolResult bool
		finalEvent *core.StreamEvent
	)
	for event := range stream.Events() {
		switch event.Type {
		case core.EventStepStart:
			stepStart = true
		case core.EventStepFinish:
			stepFinish = true
		case core.EventToolResult:
			toolResult = true
		case core.EventFinish:
			if event.StepID == 0 {
				copy := event
				finalEvent = &copy
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if !stepStart {
		t.Fatalf("expected step start events")
	}
	if !stepFinish {
		t.Fatalf("expected step finish events")
	}
	if !toolResult {
		t.Fatalf("expected tool result events")
	}
	if finalEvent == nil || finalEvent.FinishReason == nil || finalEvent.FinishReason.Type == "" {
		t.Fatalf("unexpected final finish event: %#v", finalEvent)
	}
	meta := stream.Meta()
	if meta.Provider != "fake" {
		t.Fatalf("unexpected stream meta provider: %s", meta.Provider)
	}
}

func TestRunnerOnStopFinalizer(t *testing.T) {
	provider := &fakeProvider{}
	r := New(provider, WithMaxParallel(2))

	type input struct {
		Name string `json:"name"`
	}

	tool := tools.New[input, map[string]string](
		"greet",
		"Generate greeting",
		func(ctx context.Context, in input, meta core.ToolMeta) (map[string]string, error) {
			return map[string]string{"greeting": "Hello " + in.Name}, nil
		},
	)

	finalCalled := false
	res, err := r.ExecuteRequest(context.Background(), core.Request{
		Messages: []core.Message{core.UserMessage(core.TextPart("Say hi"))},
		Tools:    []core.ToolHandle{tools.NewCoreAdapter(tool)},
		StopWhen: core.MaxSteps(1),
		OnStop: func(ctx context.Context, state core.FinalState) (*core.TextResult, error) {
			finalCalled = true
			if len(state.Steps) != 1 {
				return nil, fmt.Errorf("expected 1 step, got %d", len(state.Steps))
			}
			if state.StopReason.Type != core.StopReasonMaxSteps {
				return nil, fmt.Errorf("unexpected stop reason %s", state.StopReason.Type)
			}
			return &core.TextResult{
				Text:         "stopped",
				FinishReason: state.StopReason,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("ExecuteRequest error: %v", err)
	}
	if !finalCalled {
		t.Fatalf("expected finalizer to run")
	}
	if res.Text != "stopped" {
		t.Fatalf("unexpected final text: %s", res.Text)
	}
	if res.FinishReason.Type != core.StopReasonMaxSteps {
		t.Fatalf("unexpected finish reason: %#v", res.FinishReason)
	}
}

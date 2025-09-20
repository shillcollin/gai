package runner

import (
	"context"
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

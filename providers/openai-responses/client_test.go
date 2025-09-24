package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/shillcollin/gai/core"
)

func TestConvertMessagesIncludesFunctionCalls(t *testing.T) {
	client := &Client{opts: defaultOptions()}

	messages := []core.Message{
		core.SystemMessage("stay compliant"),
		{
			Role:  core.Assistant,
			Parts: []core.Part{core.ToolCall{ID: "call_1", Name: "lookup", Input: map[string]any{"q": "earth"}}},
		},
		{
			Role:  core.User,
			Parts: []core.Part{core.ToolResult{ID: "call_1", Result: map[string]any{"answer": 42}}},
		},
	}

	inputs, instructions, err := client.convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}

	if instructions != "stay compliant" {
		t.Fatalf("unexpected instructions: %q", instructions)
	}
	if len(inputs) != 2 {
		t.Fatalf("expected two input entries, got %d", len(inputs))
	}

	call, ok := inputs[0].(FunctionCallParam)
	if !ok {
		t.Fatalf("first input should be FunctionCallParam, got %T", inputs[0])
	}
	if call.Type != "function_call" || call.Name != "lookup" {
		t.Fatalf("unexpected call payload: %#v", call)
	}
	if call.Arguments == "" {
		t.Fatalf("call arguments should not be empty")
	}

	output, ok := inputs[1].(FunctionCallOutputParam)
	if !ok {
		t.Fatalf("second input should be FunctionCallOutputParam, got %T", inputs[1])
	}
	if output.CallID != "call_1" || output.Output == "" {
		t.Fatalf("unexpected output payload: %#v", output)
	}
}

func TestApplyProviderOptionsAudioAndModalities(t *testing.T) {
	client := &Client{opts: defaultOptions()}
	payload := &ResponsesRequest{Model: "o4-mini"}

	options := map[string]any{
		"openai-responses.modalities":          []any{"text", "audio"},
		"openai-responses.audio":               map[string]any{"voice": "alloy", "format": "wav", "sample_rate": 44100},
		"openai-responses.tool_choice":         map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
		"openai-responses.parallel_tool_calls": false,
	}

	warnings, err := client.applyProviderOptions(payload, options, policyForModel(payload.Model))
	if err != nil {
		t.Fatalf("applyProviderOptions error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	if len(payload.Modalities) != 2 || payload.Modalities[1] != "audio" {
		t.Fatalf("modalities not applied: %#v", payload.Modalities)
	}
	if payload.Audio == nil || payload.Audio.Voice != "alloy" || payload.Audio.Format != "wav" || payload.Audio.SampleRate != 44100 {
		t.Fatalf("audio params not applied: %#v", payload.Audio)
	}
	if payload.ToolChoice == nil {
		t.Fatalf("tool choice should be set")
	}
	if b := payload.ParallelToolCalls; b == nil || *b != false {
		t.Fatalf("parallel tool calls flag not applied: %#v", b)
	}
}

func TestGenerateObjectProducesJSON(t *testing.T) {
	client := New(
		WithAPIKey("sk"),
		WithHTTPClient(&http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			var payload ResponsesRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.Text == nil || payload.Text.Format == nil || payload.Text.Format.Type != "json_schema" {
				return nil, fmt.Errorf("expected json schema output format")
			}
			resp := ActualResponsesResponse{
				ID:    "resp_123",
				Model: "o4-mini",
				Output: []ActualResponseItem{{
					Type:    "message",
					Content: []ActualContentPart{{Type: "output_text", Text: "{\"answer\":42}"}},
				}},
			}
			buf, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(buf)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})}),
	)

	result, err := client.GenerateObject(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("answer"))}})
	if err != nil {
		t.Fatalf("GenerateObject error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if payload["answer"] != float64(42) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestAnnotationsToCitations(t *testing.T) {
	ann := map[string]any{
		"type":        "citation",
		"uri":         "https://example.com",
		"title":       "Example",
		"snippet":     "Sample snippet",
		"start_index": 5,
		"end_index":   12,
		"score":       0.87,
	}
	raw, _ := json.Marshal(ann)

	cites := annotationsToCitations([]json.RawMessage{raw})
	if len(cites) != 1 {
		t.Fatalf("expected one citation, got %d", len(cites))
	}
	c := cites[0]
	if c.URI != "https://example.com" || c.Title != "Example" || c.Snippet != "Sample snippet" {
		t.Fatalf("unexpected citation: %#v", c)
	}
	if c.Start != 5 || c.End != 12 || c.Score < 0.8 {
		t.Fatalf("unexpected citation indices: %#v", c)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (rt roundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt(req)
}

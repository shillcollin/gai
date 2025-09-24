package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return r(req)
}

func TestGenerateText(t *testing.T) {
	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-API-Key") != "key" {
			t.Fatalf("missing api key header")
		}
		var payload anthropicRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "claude-3-7-sonnet" {
			t.Fatalf("unexpected model")
		}
		resp := anthropicResponse{
			ID:         "msg_123",
			Model:      "claude-3-7-sonnet",
			Content:    []anthropicContent{{Type: "text", Text: "Hello"}},
			StopReason: "end_turn",
			Usage:      anthropicUsage{InputTokens: 12, OutputTokens: 4},
		}
		buf, _ := json.Marshal(resp)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(buf)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("claude-3-7-sonnet"),
		WithBaseURL("https://api.anthropic.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	res, err := client.GenerateText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hi"))}})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if res.Text != "Hello" {
		t.Fatalf("unexpected text: %s", res.Text)
	}
}

func TestStreamText(t *testing.T) {
	events := "event: content_block_delta\n" +
		"data: {\"model\":\"claude-3\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"model\":\"claude-3\",\"delta\":{\"type\":\"text_delta\",\"text\":\" there\"}}\n\n" +
		"event: message_stop\n" +
		"data: {}\n\n"

	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("claude-3"),
		WithBaseURL("https://api.anthropic.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	stream, err := client.StreamText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hi"))}})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	defer stream.Close()

	var text string
	for ev := range stream.Events() {
		if ev.Type == core.EventTextDelta {
			text += ev.TextDelta
		}
	}
	if text != "Hi there" {
		t.Fatalf("unexpected text: %s", text)
	}
}

func TestConvertMessagesWithImage(t *testing.T) {
	img := core.Image{Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x01, 0x02}, MIME: "image/png"}}
	msgs := []core.Message{{Role: core.User, Parts: []core.Part{core.Text{Text: "caption"}, img}}}
	converted, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}
	if len(converted) != 1 {
		t.Fatalf("expected single anthropic message")
	}
	parts := converted[0].Content
	if len(parts) != 2 {
		t.Fatalf("expected text+image parts, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "caption" {
		t.Fatalf("unexpected first part: %+v", parts[0])
	}
	if parts[1].Type != "image" || parts[1].Source == nil || parts[1].Source.Type != "base64" {
		t.Fatalf("unexpected image part: %+v", parts[1])
	}
}

func TestBuildPayloadWithTools(t *testing.T) {
	tool := tools.New[struct {
		City string `json:"city"`
	}]("lookup_weather", "Lookup current weather", func(ctx context.Context, in struct {
		City string `json:"city"`
	}, meta core.ToolMeta) (any, error) { return map[string]any{"status": "ok"}, nil })
	adapter := tools.NewCoreAdapter(tool)
	payload, err := convertTools([]core.ToolHandle{adapter})
	if err != nil {
		t.Fatalf("convertTools error: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected single tool, got %d", len(payload))
	}
	if payload[0].Name != "lookup_weather" || payload[0].InputSchema == nil {
		t.Fatalf("unexpected tool payload: %#v", payload[0])
	}
}

func TestStreamTextToolCall(t *testing.T) {
	events := "event: content_block_start\n" +
		"data: {\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"lookup_weather\",\"input\":{}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\\\"Paris\\\"}\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"index\":0}\n\n" +
		"event: message_stop\n" +
		"data: {}\n\n"

	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("claude-3"),
		WithBaseURL("https://api.anthropic.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	stream, err := client.StreamText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hi"))}})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	defer stream.Close()

	var toolCalls []core.ToolCall
	for ev := range stream.Events() {
		if ev.Type == core.EventToolCall {
			toolCalls = append(toolCalls, ev.ToolCall)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "lookup_weather" {
		t.Fatalf("unexpected tool name %s", toolCalls[0].Name)
	}
	if toolCalls[0].Input["city"] != "Paris" {
		t.Fatalf("unexpected tool input: %#v", toolCalls[0].Input)
	}
}

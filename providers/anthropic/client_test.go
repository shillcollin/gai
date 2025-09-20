package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/shillcollin/gai/core"
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

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/shillcollin/gai/core"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGenerateText(t *testing.T) {
	var captured map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		body := chatCompletionResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4o-mini",
			Choices: []chatCompletionChoice{{
				Message:      openAIMessage{Content: []openAIContent{{Type: "text", Text: "Hello"}}},
				FinishReason: "stop",
			}},
			Usage: openAIUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		buf, _ := json.Marshal(body)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(buf)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
		return resp, nil
	})

	client := New(
		WithBaseURL("https://api.example.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithAPIKey("test"),
		WithModel("gpt-4o-mini"),
	)

	res, err := client.GenerateText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("Hi"))}})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if res.Text != "Hello" {
		t.Fatalf("unexpected text: %s", res.Text)
	}
	if captured["model"].(string) != "gpt-4o-mini" {
		t.Fatalf("model not set in request")
	}
}

func TestStreamText(t *testing.T) {
	events := "data: {\"id\":\"chatcmpl\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"Hel\"}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"lo\"}]},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n"

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}
		return resp, nil
	})

	client := New(
		WithBaseURL("https://api.example.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithModel("gpt-4o-mini"),
	)

	stream, err := client.StreamText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("Hi"))}})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	defer stream.Close()

	var output string
	for ev := range stream.Events() {
		if ev.Type == core.EventTextDelta {
			output += ev.TextDelta
		}
	}
	if output != "Hello" {
		t.Fatalf("unexpected streamed text: %s", output)
	}
	meta := stream.Meta()
	if meta.Usage.TotalTokens != 15 || meta.Usage.InputTokens != 12 || meta.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected usage: %+v", meta.Usage)
	}
}

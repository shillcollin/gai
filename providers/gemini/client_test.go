package gemini

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

func (rt roundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt(req)
}

func TestGenerateText(t *testing.T) {
	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		resp := geminiResponse{Candidates: []geminiCandidate{{Content: geminiContent{Parts: []geminiPart{{Text: "Hi"}}}}}}
		buf, _ := json.Marshal(resp)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(buf)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("gemini-1.5-flash"),
		WithBaseURL("https://generativelanguage.googleapis.com/v1beta"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	res, err := client.GenerateText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hello"))}})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if res.Text != "Hi" {
		t.Fatalf("unexpected text: %s", res.Text)
	}
}

func TestStreamText(t *testing.T) {
	events := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"A\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"B\"}]}}]}\n\n" +
		"data: [DONE]\n\n"
	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("gemini-1.5-flash"),
		WithBaseURL("https://generativelanguage.googleapis.com/v1beta"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	stream, err := client.StreamText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hello"))}})
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
	if text != "AB" {
		t.Fatalf("unexpected text: %s", text)
	}
}

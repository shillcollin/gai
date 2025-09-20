package compat

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

func TestCompatGenerateText(t *testing.T) {
	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		resp := map[string]any{
			"id":    "chatcmpl",
			"model": "compat-model",
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "Compat"}},
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		}
		buf, _ := json.Marshal(resp)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(buf)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})

	client := New(CompatOpts{
		BaseURL:    "https://compat.example.com/v1",
		APIKey:     "key",
		Model:      "compat-model",
		HTTPClient: &http.Client{Transport: transport},
	})

	res, err := client.GenerateText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hello"))}})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if res.Text != "Compat" {
		t.Fatalf("unexpected text: %s", res.Text)
	}
}

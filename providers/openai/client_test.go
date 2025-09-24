package openai

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

func TestStreamTextEmitsToolCall(t *testing.T) {
	events := "data: {" +
		"\"id\":\"chatcmpl\"," +
		"\"model\":\"gpt-4o-mini\"," +
		"\"choices\":[{" +
		"\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"function\":{\"name\":\"fetch_weather\",\"arguments\":\"{\\\"city\\\":\\\"Berlin\\\"}\"}}]}}]," +
		"\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}" +
		"}\n\n" +
		"data: [DONE]\n\n"

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})

	client := New(
		WithBaseURL("https://api.example.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithModel("gpt-4o-mini"),
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
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	call := toolCalls[0]
	if call.Name != "fetch_weather" {
		t.Fatalf("unexpected tool name %s", call.Name)
	}
	if call.Input["city"] != "Berlin" {
		t.Fatalf("unexpected tool input: %#v", call.Input)
	}
}

func TestGenerateObjectJSON(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		if payload.ResponseFormat == nil {
			return nil, fmt.Errorf("expected response_format for json mode")
		}
		resp := chatCompletionResponse{
			ID:    "chatcmpl",
			Model: "gpt-4.1",
			Choices: []chatCompletionChoice{{
				Message: openAIMessage{Content: []openAIContent{{Type: "text", Text: "{\"answer\":42}"}}},
			}},
		}
		buf, _ := json.Marshal(resp)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(buf)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})

	client := New(
		WithModel("gpt-4.1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	res, err := client.GenerateObject(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("answer"))}})
	if err != nil {
		t.Fatalf("GenerateObject error: %v", err)
	}
	if string(res.JSON) != "{\"answer\":42}" {
		t.Fatalf("unexpected JSON: %s", res.JSON)
	}
}

func TestBuildChatPayloadMultimodal(t *testing.T) {
	client := New(WithModel("gpt-4o"))
	img := core.Image{Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x01, 0x02}, MIME: "image/png"}}
	audio := core.Audio{Format: "wav", Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x10, 0x20}, MIME: "audio/wav"}}
	video := core.Video{Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x30, 0x40}, MIME: "video/mp4"}}
	msg := core.Message{Role: core.User, Parts: []core.Part{core.Text{Text: "describe"}, img, audio, video}}
	payload, _, err := client.buildChatPayload(core.Request{Messages: []core.Message{msg}}, false)
	if err != nil {
		t.Fatalf("buildChatPayload error: %v", err)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("expected 1 message in payload")
	}
	parts := payload.Messages[0].Content
	if len(parts) != 4 {
		t.Fatalf("expected 4 content parts, got %d", len(parts))
	}
	if parts[0].Type != "text" {
		t.Fatalf("expected text part first, got %s", parts[0].Type)
	}
	if parts[1].Type != "input_image" || parts[1].Image == nil || parts[1].Image.B64JSON == "" {
		t.Fatalf("image part not encoded correctly: %+v", parts[1])
	}
	if parts[2].Type != "input_audio" || parts[2].InputAudio == nil || parts[2].InputAudio.Format != "wav" {
		t.Fatalf("audio part not encoded correctly: %+v", parts[2])
	}
	if parts[3].Type != "input_video" || parts[3].InputVideo == nil || parts[3].InputVideo.Format != "mp4" {
		t.Fatalf("video part not encoded correctly: %+v", parts[3])
	}
}

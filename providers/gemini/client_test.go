package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
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

func TestConvertMessagesWithInlineImageAndVideo(t *testing.T) {
	img := core.Image{Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x01, 0x02}, MIME: "image/png"}}
	vid := core.Video{Format: "mp4", Source: core.BlobRef{Kind: core.BlobBytes, Bytes: []byte{0x03, 0x04}, MIME: "video/mp4"}}
	msgs := []core.Message{{Role: core.User, Parts: []core.Part{img, vid}}}
	converted, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}
	if len(converted) != 1 {
		t.Fatalf("expected single content block")
	}
	parts := converted[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected two parts, got %d", len(parts))
	}
	if parts[0].InlineData == nil || parts[0].InlineData.MimeType != "image/png" {
		t.Fatalf("unexpected image part: %+v", parts[0])
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MimeType != "video/mp4" {
		t.Fatalf("unexpected video part: %+v", parts[1])
	}
}

func TestConvertMessagesImageURL(t *testing.T) {
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	}))
	defer srv.Close()
	msgs := []core.Message{{Role: core.User, Parts: []core.Part{core.ImageURL{URL: srv.URL}}}}
	converted, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}
	part := converted[0].Parts[0]
	if part.InlineData == nil || part.InlineData.MimeType != "image/jpeg" {
		t.Fatalf("unexpected inline data: %+v", part)
	}
}

func newIPv4Server(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("unable to create ipv4 test listener: %v", err)
	}
	srv := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler},
	}
	srv.Start()
	return srv
}

func TestBuildRequestWithTools(t *testing.T) {
	tool := tools.New[struct {
		City string `json:"city"`
	}]("lookup_weather", "Lookup weather", func(ctx context.Context, in struct {
		City string `json:"city"`
	}, meta core.ToolMeta) (any, error) {
		return nil, nil
	})
	adapter := tools.NewCoreAdapter(tool)
	req := core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hi"))}, Tools: []core.ToolHandle{adapter}}
	geminiReq, err := buildRequest(req, "gemini-1.5-pro")
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}
	if len(geminiReq.Tools) != 1 {
		t.Fatalf("expected one tool declaration, got %d", len(geminiReq.Tools))
	}
	decls := geminiReq.Tools[0].FunctionDeclarations
	if len(decls) != 1 || decls[0].Name != "lookup_weather" {
		t.Fatalf("unexpected declaration: %#v", decls)
	}
	if geminiReq.ToolConfig == nil || geminiReq.ToolConfig.FunctionCallingConfig == nil {
		t.Fatalf("tool config not populated")
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.Mode == "" {
		t.Fatalf("tool config missing mode")
	}
}

func TestStreamTextFunctionCall(t *testing.T) {
	events := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup_weather\",\"args\":{\"city\":\"Berlin\"}}}]}}]}\n\n" +
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
		WithModel("gemini-1.5-pro"),
		WithBaseURL("https://generativelanguage.googleapis.com/v1beta"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	stream, err := client.StreamText(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("hi"))}})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	defer stream.Close()

	var calls []core.ToolCall
	for ev := range stream.Events() {
		if ev.Type == core.EventToolCall {
			calls = append(calls, ev.ToolCall)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("expected function call, got %d", len(calls))
	}
	if calls[0].Name != "lookup_weather" || calls[0].Input["city"] != "Berlin" {
		t.Fatalf("unexpected function call payload: %#v", calls[0])
	}
}

func TestStreamTextWithArrayResponse(t *testing.T) {
	events := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"A\"}]}}]}\n\n" +
		"data: [{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"B\"}]}}]}, {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"C\"}]}}]}]\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"D\"}]}}]}\n\n" +
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

	var text strings.Builder
	var deltas []string
	for ev := range stream.Events() {
		if ev.Type == core.EventTextDelta {
			text.WriteString(ev.TextDelta)
			deltas = append(deltas, ev.TextDelta)
		}
	}

	if text.String() != "ABCD" {
		t.Fatalf("unexpected concatenated text: %s", text.String())
	}
	if len(deltas) != 4 {
		t.Fatalf("expected 4 text deltas, got %d", len(deltas))
	}
}

func TestGenerateObjectUsesJSONMimeType(t *testing.T) {
	var captured geminiRequest
	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&captured); err != nil {
			return nil, err
		}
		resp := geminiResponse{
			Candidates: []geminiCandidate{{Content: geminiContent{Parts: []geminiPart{{Text: "{\"value\":42}"}}}}},
		}
		buf, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(buf)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("gemini-2.5-pro"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	result, err := client.GenerateObject(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("json"))}})
	if err != nil {
		t.Fatalf("GenerateObject error: %v", err)
	}
	if captured.GenerationConfig.ResponseMimeType != "application/json" {
		t.Fatalf("expected response mime type application/json, got %q", captured.GenerationConfig.ResponseMimeType)
	}
	if string(result.JSON) != "{\"value\":42}" {
		t.Fatalf("unexpected JSON payload: %s", result.JSON)
	}
}

func TestStreamObjectUsesJSONMimeType(t *testing.T) {
	var captured geminiRequest
	events := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"{\\\"done\\\":true}\"}]}}]}\n\n" +
		"data: [DONE]\n\n"

	transport := roundTrip(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&captured); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(events)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})

	client := New(
		WithAPIKey("key"),
		WithModel("gemini-2.5-pro"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	objectStream, err := client.StreamObject(context.Background(), core.Request{Messages: []core.Message{core.UserMessage(core.TextPart("json"))}})
	if err != nil {
		t.Fatalf("StreamObject error: %v", err)
	}
	stream := objectStream.Stream()
	if stream == nil {
		t.Fatalf("expected underlying stream")
	}
	result, err := core.CollectStream(stream)
	if err != nil {
		t.Fatalf("collect stream: %v", err)
	}
	if strings.TrimSpace(result.Text) != "{\"done\":true}" {
		t.Fatalf("unexpected streamed text: %s", result.Text)
	}
	if captured.GenerationConfig.ResponseMimeType != "application/json" {
		t.Fatalf("expected response mime type application/json, got %q", captured.GenerationConfig.ResponseMimeType)
	}
}

func TestConvertMessagesPreservesFunctionCallThoughtSignature(t *testing.T) {
	content := geminiContent{
		Role: "model",
		Parts: []geminiPart{
			{
				FunctionCall:     &geminiFunctionCall{Name: "lookup_weather", Args: map[string]any{"city": "Paris"}},
				ThoughtSignature: "sig-123",
			},
		},
	}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	call := core.ToolCall{
		ID:    "call_1",
		Name:  "lookup_weather",
		Input: map[string]any{"city": "Paris"},
		Metadata: map[string]any{
			metadataKeyRawContent:       string(raw),
			metadataKeyThoughtSignature: "sig-123",
		},
	}
	msg := core.Message{
		Role: core.Assistant,
		Parts: []core.Part{
			call,
		},
		Metadata: map[string]any{metadataKeyRawContent: string(raw)},
	}
	converted, err := convertMessages([]core.Message{msg})
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}
	if len(converted) != 1 {
		t.Fatalf("expected single content block, got %d", len(converted))
	}
	parts := converted[0].Parts
	if len(parts) != 1 {
		t.Fatalf("expected single part, got %d", len(parts))
	}
	if parts[0].FunctionCall == nil {
		t.Fatalf("expected function call part")
	}
	if parts[0].FunctionCall.Name != "lookup_weather" {
		t.Fatalf("unexpected function name: %s", parts[0].FunctionCall.Name)
	}
	if parts[0].ThoughtSignature != "sig-123" {
		t.Fatalf("thought signature not preserved: %q", parts[0].ThoughtSignature)
	}
}

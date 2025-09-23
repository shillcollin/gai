package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
	"github.com/shillcollin/gai/obs"
	"go.opentelemetry.io/otel/attribute"
)

type Client struct {
	opts       options
	httpClient *http.Client
}

func New(opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	if o.httpClient == nil {
		o.httpClient = httpclient.New(httpclient.WithTimeout(o.timeout))
	}
	return &Client{opts: o, httpClient: o.httpClient}
}

func (c *Client) GenerateText(ctx context.Context, req core.Request) (_ *core.TextResult, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.gemini.GenerateText",
		attribute.String("ai.provider", "gemini"),
		attribute.String("ai.operation", "generateContent"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		recorder.End(err, usageTokens)
	}()

	model := chooseModel(req.Model, c.opts.model)
	payload, err := buildRequest(req, model)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))
	body, err := c.doRequest(ctx, payload, false)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp geminiResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	text := resp.JoinText()
	if text == "" {
		return nil, errors.New("gemini: empty response")
	}
	if len(resp.Candidates) > 0 {
		usageTokens = obs.UsageFromCore(core.Usage{})
	}
	return &core.TextResult{
		Text:         text,
		Model:        model,
		Provider:     "gemini",
		FinishReason: core.StopReason{Type: resp.Candidates[0].FinishReason},
	}, nil
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.gemini.StreamText",
		attribute.String("ai.provider", "gemini"),
		attribute.String("ai.operation", "streamGenerateContent"),
	)
	model := chooseModel(req.Model, c.opts.model)
	payload, err := buildRequest(req, model)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))
	body, err := c.doRequest(ctx, payload, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	stream := core.NewStream(ctx, 64)
	go func() {
		consumeStream(body, stream)
		meta := stream.Meta()
		recorder.End(stream.Err(), obs.UsageFromCore(meta.Usage))
	}()
	return stream, nil
}

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	res, err := c.GenerateText(ctx, req)
	if err != nil {
		return nil, err
	}
	return &core.ObjectResultRaw{JSON: []byte(res.Text), Model: res.Model, Provider: res.Provider, Usage: res.Usage}, nil
}

func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	stream, err := c.StreamText(ctx, req)
	if err != nil {
		return nil, err
	}
	return core.NewObjectStreamRaw(stream), nil
}

func (c *Client) Capabilities() core.Capabilities {
	return core.Capabilities{
		Streaming:  true,
		StrictJSON: false,
		Provider:   "gemini",
		Citations:  true,
	}
}

func (c *Client) doRequest(ctx context.Context, payload *geminiRequest, stream bool) (io.ReadCloser, error) {
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		return nil, err
	}
	endpoint := "/models/" + url.PathEscape(payload.Model)
	if stream {
		endpoint += ":streamGenerateContent"
	} else {
		endpoint += ":generateContent"
	}
	fullURL := strings.TrimRight(c.opts.baseURL, "/") + endpoint
	if c.opts.apiKey != "" {
		fullURL += "?key=" + url.QueryEscape(c.opts.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gemini: %s: %s", resp.Status, data)
	}
	return resp.Body, nil
}

func buildRequest(req core.Request, model string) (*geminiRequest, error) {
	contents, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		return nil, errors.New("gemini: request requires messages")
	}
	return &geminiRequest{
		Model:    model,
		Contents: contents,
		GenerationConfig: geminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
			TopP:            req.TopP,
		},
		SafetySettings: convertSafety(req.Safety),
	}, nil
}

func consumeStream(body io.ReadCloser, stream *core.Stream) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	seq := 0
	var buffer strings.Builder

	flushBuffer := func() {
		buffer.Reset()
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "[DONE]" {
			seq++
			stream.Push(core.StreamEvent{Type: core.EventFinish, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini"})
			flushBuffer()
			break
		}
		if buffer.Len() > 0 {
			buffer.WriteByte('\n')
		}
		buffer.WriteString(line)

		var resp geminiStreamResponse
		if err := json.Unmarshal([]byte(buffer.String()), &resp); err != nil {
			if strings.Contains(err.Error(), "unexpected end of JSON input") {
				continue
			}
			if strings.Contains(err.Error(), "cannot unmarshal array") {
				var respArr []geminiStreamResponse
				if errArr := json.Unmarshal([]byte(buffer.String()), &respArr); errArr == nil {
					for _, item := range respArr {
						seq++
						if text := item.JoinText(); text != "" {
							stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: text, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini"})
						}
					}
					flushBuffer()
					continue
				}
			}
			seq++
			stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini"})
			flushBuffer()
			continue
		}

		seq++
		if text := resp.JoinText(); text != "" {
			stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: text, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini"})
		}
		flushBuffer()
	}
	if err := scanner.Err(); err != nil {
		stream.Fail(err)
	}
}

func convertMessages(messages []core.Message) ([]geminiContent, error) {
	contents := make([]geminiContent, 0, len(messages))
	var systemBuffer strings.Builder

	appendContent := func(role string, parts []geminiPart) {
		if len(parts) == 0 {
			return
		}
		contents = append(contents, geminiContent{Role: role, Parts: parts})
	}

	for _, message := range messages {
		// Collect system instructions separately.
		if message.Role == core.System {
			for _, part := range message.Parts {
				if text, ok := part.(core.Text); ok {
					if systemBuffer.Len() > 0 {
						systemBuffer.WriteString("\n")
					}
					systemBuffer.WriteString(text.Text)
				}
			}
			continue
		}

		role := "user"
		switch message.Role {
		case core.User:
			role = "user"
		case core.Assistant:
			role = "model"
		default:
			role = "user"
		}

		parts := make([]geminiPart, 0, len(message.Parts))
		for _, part := range message.Parts {
			switch p := part.(type) {
			case core.Text:
				parts = append(parts, geminiPart{Text: p.Text})
			default:
				return nil, fmt.Errorf("unsupported gemini part type %T", part)
			}
		}
		appendContent(role, parts)
	}

	if systemBuffer.Len() > 0 {
		systemPart := geminiPart{Text: systemBuffer.String()}
		if len(contents) > 0 && contents[0].Role == "user" {
			contents[0].Parts = append([]geminiPart{systemPart}, contents[0].Parts...)
		} else {
			contents = append([]geminiContent{{Role: "user", Parts: []geminiPart{systemPart}}}, contents...)
		}
	}

	return contents, nil
}

func convertSafety(cfg *core.SafetyConfig) []geminiSafetySetting {
	if cfg == nil {
		return nil
	}
	// Gemini expects explicit categories; we map known ones.
	return []geminiSafetySetting{
		{Category: "HARM_CATEGORY_HARASSMENT", Threshold: toThreshold(cfg.Harassment)},
		{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: toThreshold(cfg.Hate)},
		{Category: "HARM_CATEGORY_SEXUAL_CONTENT", Threshold: toThreshold(cfg.Sexual)},
		{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: toThreshold(cfg.Dangerous)},
	}
}

func toThreshold(level core.SafetyLevel) string {
	switch level {
	case core.SafetyHigh:
		return "BLOCK_LOW_AND_ABOVE"
	case core.SafetyMedium:
		return "BLOCK_MEDIUM_AND_ABOVE"
	case core.SafetyLow:
		return "BLOCK_ONLY_HIGH"
	case core.SafetyNone:
		return "BLOCK_NONE"
	default:
		return "BLOCK_MEDIUM_AND_ABOVE"
	}
}

func chooseModel(request, fallback string) string {
	if request != "" {
		return request
	}
	return fallback
}

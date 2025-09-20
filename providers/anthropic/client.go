package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
)

// Client implements the core.Provider interface for Anthropic's Messages API.
type Client struct {
	opts       options
	httpClient *http.Client
}

// New constructs a new Anthropic client.
func New(opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	if o.httpClient == nil {
		o.httpClient = httpclient.New(httpclient.WithTimeout(o.timeout))
	}
	if _, ok := o.headers["anthropic-version"]; !ok {
		o.headers["anthropic-version"] = "2023-06-01"
	}
	return &Client{opts: o, httpClient: o.httpClient}
}

func (c *Client) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp anthropicResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	text := resp.JoinText()
	return &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "anthropic",
		Usage:        resp.Usage.toCore(),
		FinishReason: core.StopReason{Type: resp.StopReason},
	}, nil
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	payload, err := c.buildPayload(req, true)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	stream := core.NewStream(ctx, 64)
	go c.consumeStream(body, stream)
	return stream, nil
}

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	res, err := c.GenerateText(ctx, req)
	if err != nil {
		return nil, err
	}
	return &core.ObjectResultRaw{
		JSON:     []byte(res.Text),
		Model:    res.Model,
		Provider: res.Provider,
		Usage:    res.Usage,
	}, nil
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
		Streaming:         true,
		ParallelToolCalls: true,
		StrictJSON:        false,
		Provider:          "anthropic",
		Reasoning:         true,
	}
}

func (c *Client) buildPayload(req core.Request, stream bool) (*anthropicRequest, error) {
	systemPrompt, messages := splitSystem(req.Messages)
	converted, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}
	if len(converted) == 0 {
		return nil, errors.New("anthropic: request requires at least one user message")
	}
	payload := &anthropicRequest{
		Model:       chooseModel(req.Model, c.opts.model),
		MaxTokens:   chooseMaxTokens(req.MaxTokens),
		Messages:    converted,
		Stream:      stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		System:      systemPrompt,
	}
	if len(req.Messages) > 0 {
		payload.Metadata = req.Metadata
	}
	return payload, nil
}

func (c *Client) doRequest(ctx context.Context, payload *anthropicRequest) (io.ReadCloser, error) {
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.opts.baseURL, "/")+"/messages", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.opts.apiKey)
	for k, v := range c.opts.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, data)
	}
	return resp.Body, nil
}

func (c *Client) consumeStream(body io.ReadCloser, stream *core.Stream) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	var currentEvent string
	seq := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		seq++
		switch currentEvent {
		case "content_block_delta":
			var delta anthropicContentDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now()})
				continue
			}
			if delta.Delta.Text != "" {
				stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: delta.Delta.Text, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic", Model: delta.Model})
			}
		case "message_stop":
			stream.Push(core.StreamEvent{Type: core.EventFinish, FinishReason: &core.StopReason{Type: "stop"}, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic"})
		case "error":
			stream.Push(core.StreamEvent{Type: core.EventError, Error: fmt.Errorf("anthropic stream error: %s", data), Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic"})
		}
	}
	if err := scanner.Err(); err != nil {
		stream.Fail(err)
	}
}

func splitSystem(messages []core.Message) (string, []core.Message) {
	var systemParts []string
	filtered := make([]core.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == core.System {
			for _, part := range msg.Parts {
				if text, ok := part.(core.Text); ok {
					systemParts = append(systemParts, text.Text)
				}
			}
			continue
		}
		filtered = append(filtered, msg)
	}
	return strings.Join(systemParts, "\n"), filtered
}

func convertMessages(messages []core.Message) ([]anthropicMessage, error) {
	out := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		role := roleString(msg.Role)
		if role == "" {
			return nil, fmt.Errorf("unsupported role %s", msg.Role)
		}
		content := make([]anthropicContent, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case core.Text:
				content = append(content, anthropicContent{Type: "text", Text: p.Text})
			default:
				return nil, fmt.Errorf("unsupported part type %T", part)
			}
		}
		out = append(out, anthropicMessage{Role: role, Content: content})
	}
	return out, nil
}

func chooseModel(requestModel, defaultModel string) string {
	if requestModel != "" {
		return requestModel
	}
	return defaultModel
}

func chooseMaxTokens(maxTokens int) int {
	if maxTokens > 0 {
		return maxTokens
	}
	return 1024
}

func roleString(role core.Role) string {
	switch role {
	case core.User:
		return "user"
	case core.Assistant:
		return "assistant"
	default:
		return ""
	}
}

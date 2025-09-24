package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/schema"
	"go.opentelemetry.io/otel/attribute"
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

func (c *Client) GenerateText(ctx context.Context, req core.Request) (_ *core.TextResult, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.anthropic.GenerateText",
		attribute.String("ai.provider", "anthropic"),
		attribute.String("ai.operation", "messages"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		recorder.End(err, usageTokens)
	}()

	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))
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
	usageTokens = obs.UsageFromCore(resp.Usage.toCore())
	return &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "anthropic",
		Usage:        resp.Usage.toCore(),
		FinishReason: core.StopReason{Type: resp.StopReason},
	}, nil
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.anthropic.StreamText",
		attribute.String("ai.provider", "anthropic"),
		attribute.String("ai.operation", "messages.stream"),
	)
	payload, err := c.buildPayload(req, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))
	body, err := c.doRequest(ctx, payload)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	stream := core.NewStream(ctx, 64)
	go func() {
		c.consumeStream(body, stream)
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
	if len(req.Tools) > 0 {
		tools, err := convertTools(req.Tools)
		if err != nil {
			return nil, err
		}
		payload.Tools = tools
		switch req.ToolChoice {
		case core.ToolChoiceNone:
			payload.ToolChoice = map[string]string{"type": "none"}
		case core.ToolChoiceRequired:
			payload.ToolChoice = map[string]string{"type": "any"}
		default:
			payload.ToolChoice = map[string]string{"type": "auto"}
		}
	}
	// Anthropic rejects arbitrary metadata keys ("...: Extra inputs are not permitted").
	// Omit metadata to preserve compatibility until official guidance changes.
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
	usage := anthropicUsage{}
	var finishReason string
	var model string
	type toolAccumulator struct {
		id     string
		name   string
		input  map[string]any
		buffer strings.Builder
	}
	toolBlocks := map[int]*toolAccumulator{}
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
		case "content_block_start":
			var start anthropicContentBlockStart
			if err := json.Unmarshal([]byte(data), &start); err != nil {
				stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic"})
				continue
			}
			if start.ContentBlock.Type == "tool_use" {
				acc := &toolAccumulator{
					id:    start.ContentBlock.ID,
					name:  start.ContentBlock.Name,
					input: start.ContentBlock.Input,
				}
				toolBlocks[start.Index] = acc
			}
		case "content_block_delta":
			var delta anthropicContentDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now()})
				continue
			}
			if delta.Model != "" {
				model = delta.Model
			}
			switch delta.Delta.Type {
			case "text_delta":
				if delta.Delta.Text != "" {
					stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: delta.Delta.Text, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic", Model: delta.Model})
				}
			case "input_json_delta":
				if acc, ok := toolBlocks[delta.Index]; ok {
					acc.buffer.WriteString(delta.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			var stop anthropicContentBlockStop
			if err := json.Unmarshal([]byte(data), &stop); err != nil {
				stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic"})
				continue
			}
			if acc, ok := toolBlocks[stop.Index]; ok {
				args := acc.input
				payload := strings.TrimSpace(acc.buffer.String())
				if payload != "" {
					var parsed map[string]any
					if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
						args = parsed
					}
				}
				if args == nil {
					args = map[string]any{}
				}
				id := acc.id
				if id == "" {
					id = fmt.Sprintf("tool-%d", stop.Index)
				}
				stream.Push(core.StreamEvent{
					Type: core.EventToolCall,
					ToolCall: core.ToolCall{
						ID:    id,
						Name:  acc.name,
						Input: args,
					},
					Schema:    "gai.events.v1",
					Seq:       seq,
					Timestamp: time.Now(),
					Provider:  "anthropic",
					Model:     model,
				})
				delete(toolBlocks, stop.Index)
			}
		case "message_delta":
			var delta anthropicMessageDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic", Model: model})
				continue
			}
			if delta.Model != "" {
				model = delta.Model
			}
			if delta.Usage.InputTokens > usage.InputTokens {
				usage.InputTokens = delta.Usage.InputTokens
			}
			if delta.Usage.OutputTokens > usage.OutputTokens {
				usage.OutputTokens = delta.Usage.OutputTokens
			}
			if delta.Delta.StopReason != nil && *delta.Delta.StopReason != "" {
				finishReason = *delta.Delta.StopReason
			}
		case "message_stop":
			reason := finishReason
			if reason == "" {
				reason = "stop"
			}
			stream.Push(core.StreamEvent{Type: core.EventFinish, FinishReason: &core.StopReason{Type: reason}, Schema: "gai.events.v1", Seq: seq, Timestamp: time.Now(), Provider: "anthropic", Model: model, Usage: usage.toCore()})
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
			case core.Image:
				source, err := buildImageSource(p.Source)
				if err != nil {
					return nil, err
				}
				content = append(content, anthropicContent{Type: "image", Source: source})
			case core.ImageURL:
				source, err := buildImageURLSource(p)
				if err != nil {
					return nil, err
				}
				content = append(content, anthropicContent{Type: "image", Source: source})
			case core.ToolCall:
				input := p.Input
				if input == nil {
					input = map[string]any{}
				}
				content = append(content, anthropicContent{
					Type:  "tool_use",
					Name:  p.Name,
					ID:    p.ID,
					Input: input,
				})
			case core.ToolResult:
				var output any
				switch v := p.Result.(type) {
				case nil:
					output = ""
				case string:
					output = v
				default:
					if data, err := json.Marshal(v); err == nil {
						output = string(data)
					} else {
						output = fmt.Sprintf("%v", v)
					}
				}
				if output == "" && p.Error != "" {
					output = p.Error
				}
				content = append(content, anthropicContent{
					Type:      "tool_result",
					ToolUseID: p.ID,
					Content:   output,
					IsError:   p.Error != "",
				})
			default:
				return nil, fmt.Errorf("unsupported part type %T", part)
			}
		}
		out = append(out, anthropicMessage{Role: role, Content: content})
	}
	return out, nil
}

func convertTools(handles []core.ToolHandle) ([]anthropicTool, error) {
	tools := make([]anthropicTool, 0, len(handles))
	for _, handle := range handles {
		if handle == nil {
			continue
		}
		schemaMap, err := schemaToMap(handle.InputSchema())
		if err != nil {
			return nil, err
		}
		tools = append(tools, anthropicTool{
			Name:        handle.Name(),
			Description: handle.Description(),
			InputSchema: schemaMap,
		})
	}
	return tools, nil
}

func schemaToMap(s *schema.Schema) (map[string]any, error) {
	if s == nil {
		return map[string]any{"type": "object"}, nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	return out, nil
}

func buildImageSource(blob core.BlobRef) (*anthropicImageSource, error) {
	if err := blob.Validate(); err != nil {
		return nil, fmt.Errorf("validate image blob: %w", err)
	}
	if blob.Kind == core.BlobProvider {
		return nil, errors.New("anthropic image requires inline data or URL; provider references unsupported")
	}
	data, err := blob.Read()
	if err != nil {
		return nil, fmt.Errorf("read image blob: %w", err)
	}
	if blob.MIME == "" {
		return nil, errors.New("image MIME type required for anthropic")
	}
	return &anthropicImageSource{
		Type:      "base64",
		MediaType: blob.MIME,
		Data:      base64.StdEncoding.EncodeToString(data),
	}, nil
}

func buildImageURLSource(img core.ImageURL) (*anthropicImageSource, error) {
	if strings.TrimSpace(img.URL) == "" {
		return nil, errors.New("image url is required")
	}
	source := &anthropicImageSource{
		Type: "url",
		URL:  img.URL,
	}
	if img.MIME != "" {
		source.MediaType = img.MIME
	}
	return source, nil
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

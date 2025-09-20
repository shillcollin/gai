package openai

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
	"github.com/shillcollin/gai/schema"
)

// Client implements the core.Provider interface for OpenAI's chat completions API.
type Client struct {
	httpClient *http.Client
	opts       options
}

// New constructs a new OpenAI client.
func New(opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	if o.httpClient == nil {
		o.httpClient = httpclient.New(httpclient.WithTimeout(o.timeout))
	}
	return &Client{
		httpClient: o.httpClient,
		opts:       o,
	}
}

func (c *Client) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	payload, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp chatCompletionResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("openai: empty choices")
	}
	choice := resp.Choices[0]
	text := choice.Message.JoinText()
	usage := resp.Usage.toCore()
	finish := core.StopReason{Type: choice.FinishReason}

	return &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "openai",
		Usage:        usage,
		FinishReason: finish,
	}, nil
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	payload, err := c.buildChatPayload(req, true)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		return nil, err
	}

	stream := core.NewStream(ctx, 64)
	go c.consumeStream(ctx, body, stream)
	return stream, nil
}

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}
	req.ProviderOptions["response_format"] = map[string]any{"type": "json_object"}
	payload, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}
	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp chatCompletionResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("openai: empty choices")
	}
	content := resp.Choices[0].Message.JoinText()
	return &core.ObjectResultRaw{
		JSON:     []byte(content),
		Model:    resp.Model,
		Provider: "openai",
		Usage:    resp.Usage.toCore(),
	}, nil
}

func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	payload, err := c.buildChatPayload(req, true)
	if err != nil {
		return nil, err
	}
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}
	req.ProviderOptions["response_format"] = map[string]any{"type": "json_object"}

	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	stream := core.NewStream(ctx, 64)
	go c.consumeStream(ctx, body, stream)
	return core.NewObjectStreamRaw(stream), nil
}

func (c *Client) Capabilities() core.Capabilities {
	return core.Capabilities{
		Streaming:         true,
		ParallelToolCalls: true,
		StrictJSON:        true,
		Provider:          "openai",
	}
}

func (c *Client) buildChatPayload(req core.Request, stream bool) (*chatCompletionRequest, error) {
	messages, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := convertTools(req.Tools)
	if err != nil {
		return nil, err
	}

	payload := &chatCompletionRequest{
		Model:       chooseModel(req.Model, c.opts.model),
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stream:      stream,
	}

	if len(tools) > 0 {
		payload.Tools = tools
		payload.ToolChoice = convertToolChoice(req.ToolChoice)
	}
	if req.TopK > 0 {
		payload.TopK = req.TopK
	}
	if req.Safety != nil {
		payload.SafetySettings = convertSafety(req.Safety)
	}
	if err := applyProviderOptions(payload, req.ProviderOptions); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, payload any) (io.ReadCloser, error) {
	buf := &bytes.Buffer{}
	if payload != nil {
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.opts.baseURL, "/")+path, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.opts.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.apiKey)
	}
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
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, data)
	}
	return resp.Body, nil
}

func (c *Client) consumeStream(ctx context.Context, body io.ReadCloser, stream *core.Stream) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	seq := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			stream.Push(core.StreamEvent{Type: core.EventFinish, Timestamp: time.Now(), Schema: "gai.events.v1", Seq: seq, StreamID: "stream"})
			return
		}
		seq++
		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Timestamp: time.Now(), Schema: "gai.events.v1", Seq: seq})
			continue
		}
		if len(delta.Choices) == 0 {
			continue
		}
		choice := delta.Choices[0]
		if text := choice.Delta.JoinText(); text != "" {
			stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: text, Timestamp: time.Now(), Schema: "gai.events.v1", Seq: seq, StreamID: delta.ID, Model: delta.Model, Provider: "openai"})
		}
		if len(choice.Delta.ToolCalls) > 0 {
			for _, call := range choice.Delta.ToolCalls {
				args, err := parseArguments(call.Function.Arguments)
				if err != nil {
					stream.Push(core.StreamEvent{Type: core.EventError, Error: err, Timestamp: time.Now(), Schema: "gai.events.v1", Seq: seq, StreamID: delta.ID})
					continue
				}
				stream.Push(core.StreamEvent{
					Type:      core.EventToolCall,
					ToolCall:  core.ToolCall{ID: call.ID, Name: call.Function.Name, Input: args},
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  delta.ID,
					Model:     delta.Model,
					Provider:  "openai",
				})
			}
		}
		if choice.FinishReason != "" {
			stream.Push(core.StreamEvent{Type: core.EventFinish, FinishReason: &core.StopReason{Type: choice.FinishReason}, Timestamp: time.Now(), Schema: "gai.events.v1", Seq: seq, StreamID: delta.ID, Model: delta.Model, Provider: "openai"})
		}
	}
	if err := scanner.Err(); err != nil {
		stream.Fail(err)
		return
	}
}

func convertMessages(messages []core.Message) ([]openAIMessage, error) {
	result := make([]openAIMessage, 0, len(messages))
	for _, msg := range messages {
		toolResults := extractToolResults(msg)
		if len(toolResults) > 0 {
			result = append(result, toolResults...)
			continue
		}
		omsg := openAIMessage{
			Role: roleString(msg.Role),
			Name: msg.Name,
		}
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case core.Text:
				omsg.Content = append(omsg.Content, openAIContent{Type: "text", Text: p.Text})
			case core.ImageURL:
				omsg.Content = append(omsg.Content, openAIContent{Type: "image_url", ImageURL: &openAIImageURL{URL: p.URL, Detail: p.Detail}})
			case core.ToolCall:
				args, err := json.Marshal(p.Input)
				if err != nil {
					return nil, fmt.Errorf("marshal tool input: %w", err)
				}
				omsg.ToolCalls = append(omsg.ToolCalls, openAIToolCall{
					ID:   p.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      p.Name,
						Arguments: string(args),
					},
				})
			default:
				return nil, fmt.Errorf("unsupported part type %T", part)
			}
		}
		result = append(result, omsg)
	}
	return result, nil
}

func extractToolResults(msg core.Message) []openAIMessage {
	var results []openAIMessage
	for _, part := range msg.Parts {
		tr, ok := part.(core.ToolResult)
		if !ok {
			continue
		}
		var text string
		switch v := tr.Result.(type) {
		case string:
			text = v
		default:
			payload, err := json.Marshal(v)
			if err != nil {
				text = fmt.Sprintf("%v", v)
			} else {
				text = string(payload)
			}
		}
		results = append(results, openAIMessage{
			Role:       "tool",
			ToolCallID: tr.ID,
			Content:    []openAIContent{{Type: "text", Text: text}},
		})
	}
	return results
}

func convertTools(tools []core.ToolHandle) ([]openAITool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		params, err := schemaToMap(tool.InputSchema())
		if err != nil {
			return nil, err
		}
		out = append(out, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  params,
			},
		})
	}
	return out, nil
}

func schemaToMap(s *schema.Schema) (map[string]any, error) {
	if s == nil {
		return map[string]any{"type": "object"}, nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	return result, nil
}

func convertToolChoice(choice core.ToolChoice) any {
	switch choice {
	case core.ToolChoiceRequired:
		return map[string]any{"type": "function"}
	case core.ToolChoiceNone:
		return "none"
	default:
		return "auto"
	}
}

func convertSafety(cfg *core.SafetyConfig) map[string]any {
	if cfg == nil {
		return nil
	}
	return map[string]any{
		"harassment": cfg.Harassment,
		"hate":       cfg.Hate,
		"sexual":     cfg.Sexual,
		"dangerous":  cfg.Dangerous,
		"self_harm":  cfg.SelfHarm,
		"other":      cfg.Other,
	}
}

func chooseModel(requestModel, defaultModel string) string {
	if requestModel != "" {
		return requestModel
	}
	return defaultModel
}

func roleString(role core.Role) string {
	switch role {
	case core.System:
		return "system"
	case core.User:
		return "user"
	case core.Assistant:
		return "assistant"
	default:
		return "user"
	}
}

func applyProviderOptions(payload *chatCompletionRequest, opts map[string]any) error {
	if payload == nil || len(opts) == 0 {
		return nil
	}
	for key, value := range opts {
		switch key {
		case "response_format", "openai.response_format":
			payload.ResponseFormat = value
		case "presence_penalty", "openai.presence_penalty":
			if f, err := toFloat32(value); err == nil {
				payload.PresencePenalty = f
			} else {
				return fmt.Errorf("presence_penalty: %w", err)
			}
		case "frequency_penalty", "openai.frequency_penalty":
			if f, err := toFloat32(value); err == nil {
				payload.FrequencyPenalty = f
			} else {
				return fmt.Errorf("frequency_penalty: %w", err)
			}
		case "logit_bias", "openai.logit_bias":
			bias, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("logit_bias must be object")
			}
			payload.LogitBias = map[string]float32{}
			for token, raw := range bias {
				f, err := toFloat32(raw)
				if err != nil {
					return fmt.Errorf("logit_bias[%s]: %w", token, err)
				}
				payload.LogitBias[token] = f
			}
		case "user", "openai.user":
			if s, ok := value.(string); ok {
				payload.User = s
			} else {
				return fmt.Errorf("user must be string")
			}
		case "seed", "openai.seed":
			if i, err := toInt(value); err == nil {
				payload.Seed = &i
			} else {
				return fmt.Errorf("seed: %w", err)
			}
		case "stop", "openai.stop":
			stops, err := toStringSlice(value)
			if err != nil {
				return fmt.Errorf("stop: %w", err)
			}
			payload.Stop = stops
		default:
			// ignore unknown options for forward compatibility
		}
	}
	return nil
}

func toFloat32(value any) (float32, error) {
	switch v := value.(type) {
	case float32:
		return v, nil
	case float64:
		return float32(v), nil
	case json.Number:
		f, err := v.Float64()
		return float32(f), err
	case int:
		return float32(v), nil
	case int64:
		return float32(v), nil
	default:
		return 0, fmt.Errorf("invalid number type %T", value)
	}
}

func toInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("invalid integer type %T", value)
	}
}

func toStringSlice(value any) ([]string, error) {
	switch v := value.(type) {
	case string:
		return []string{v}, nil
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("stop[%d] not string", i)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid stop type %T", value)
	}
}

func parseArguments(input string) (map[string]any, error) {
	if strings.TrimSpace(input) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(input), &out); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	return out, nil
}

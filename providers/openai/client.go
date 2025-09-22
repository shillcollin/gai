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
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/schema"
	"go.opentelemetry.io/otel/attribute"
)

// Client implements the core.Provider interface for OpenAI's chat completions API.
type Client struct {
	httpClient *http.Client
	opts       options
}

func warnDroppedParam(field, model, reason string) core.Warning {
	return core.Warning{
		Code:    "param_dropped",
		Field:   field,
		Message: fmt.Sprintf("%s ignored for model %s (%s)", field, model, reason),
	}
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

func (c *Client) GenerateText(ctx context.Context, req core.Request) (_ *core.TextResult, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai.GenerateText",
		attribute.String("ai.provider", "openai"),
		attribute.String("ai.operation", "chat.completions"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		if recorder != nil {
			recorder.End(err, usageTokens)
		}
	}()

	payload, warnings, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

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
	usageTokens = obs.UsageFromCore(usage)
	finish := core.StopReason{Type: choice.FinishReason}

	result := &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "openai",
		Usage:        usage,
		FinishReason: finish,
	}
	if len(warnings) > 0 {
		result.Warnings = append(result.Warnings, warnings...)
	}
	return result, nil
}

func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai.StreamText",
		attribute.String("ai.provider", "openai"),
		attribute.String("ai.operation", "chat.completions.stream"),
	)
	payload, warnings, err := c.buildChatPayload(req, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}

	stream := core.NewStream(ctx, 64)
	if len(warnings) > 0 {
		stream.AddWarnings(warnings...)
	}
	go func() {
		c.consumeStream(ctx, body, stream)
		meta := stream.Meta()
		recorder.End(stream.Err(), obs.UsageFromCore(meta.Usage))
	}()
	return stream, nil
}

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (_ *core.ObjectResultRaw, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai.GenerateObject",
		attribute.String("ai.provider", "openai"),
		attribute.String("ai.operation", "chat.completions.json"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		if recorder != nil {
			recorder.End(err, usageTokens)
		}
	}()

	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}
	req.ProviderOptions["response_format"] = map[string]any{"type": "json_object"}
	payload, warnings, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

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
	usageTokens = obs.UsageFromCore(resp.Usage.toCore())
	result := &core.ObjectResultRaw{
		JSON:     []byte(content),
		Model:    resp.Model,
		Provider: "openai",
		Usage:    resp.Usage.toCore(),
	}
	if len(warnings) > 0 {
		result.Warnings = append(result.Warnings, warnings...)
	}
	return result, nil
}

func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai.StreamObject",
		attribute.String("ai.provider", "openai"),
		attribute.String("ai.operation", "chat.completions.json.stream"),
	)
	payload, warnings, err := c.buildChatPayload(req, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}
	req.ProviderOptions["response_format"] = map[string]any{"type": "json_object"}

	body, err := c.doRequest(ctx, "POST", "/chat/completions", payload)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	stream := core.NewStream(ctx, 64)
	if len(warnings) > 0 {
		stream.AddWarnings(warnings...)
	}
	go func() {
		c.consumeStream(ctx, body, stream)
		meta := stream.Meta()
		recorder.End(stream.Err(), obs.UsageFromCore(meta.Usage))
	}()
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

func (c *Client) buildChatPayload(req core.Request, stream bool) (*chatCompletionRequest, []core.Warning, error) {
	messages, err := convertMessages(req.Messages)
	if err != nil {
		return nil, nil, err
	}
	tools, err := convertTools(req.Tools)
	if err != nil {
		return nil, nil, err
	}

	model := chooseModel(req.Model, c.opts.model)
	profile := profileForModel(model)
	payload := &chatCompletionRequest{
		Model:    model,
		Messages: messages,
		Stream:   stream,
	}
	warnings := make([]core.Warning, 0, 2)

	if req.MaxTokens > 0 {
		if profile.UseMaxCompletionTokens {
			payload.MaxCompletionTokens = req.MaxTokens
		} else {
			payload.MaxTokens = req.MaxTokens
		}
	}
	if req.Temperature != 0 {
		if profile.AllowTemperature {
			payload.Temperature = req.Temperature
		} else {
			warnings = append(warnings, warnDroppedParam("temperature", model, "temperature is managed automatically"))
		}
	}
	if req.TopP != 0 {
		if profile.AllowTopP {
			payload.TopP = req.TopP
		} else {
			warnings = append(warnings, warnDroppedParam("top_p", model, "top_p is not supported"))
		}
	}
	if req.TopK > 0 {
		if profile.AllowTopK {
			payload.TopK = req.TopK
		} else {
			warnings = append(warnings, warnDroppedParam("top_k", model, "top_k is not supported"))
		}
	}

	if len(tools) > 0 {
		payload.Tools = tools
		payload.ToolChoice = convertToolChoice(req.ToolChoice)
	}
	if req.Safety != nil {
		payload.SafetySettings = convertSafety(req.Safety)
	}
	optionWarnings, err := applyProviderOptions(payload, req.ProviderOptions, profile)
	if err != nil {
		return nil, nil, err
	}
	if len(optionWarnings) > 0 {
		warnings = append(warnings, optionWarnings...)
	}

	return payload, warnings, nil
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
	type toolAccumulator struct {
		buf  *strings.Builder
		id   string
		name string
	}
	toolAcc := make(map[int]*toolAccumulator)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			for idx, acc := range toolAcc {
				if acc == nil || acc.buf == nil {
					continue
				}
				args, err := parseArguments(acc.buf.String())
				if err != nil || acc.name == "" {
					continue
				}
				id := acc.id
				if id == "" {
					id = fmt.Sprintf("index_%d", idx)
				}
				stream.Push(core.StreamEvent{
					Type: core.EventToolCall,
					ToolCall: core.ToolCall{
						ID:    id,
						Name:  acc.name,
						Input: args,
					},
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  "stream",
					Provider:  "openai",
				})
			}
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
			for i, call := range choice.Delta.ToolCalls {
				acc, ok := toolAcc[i]
				if !ok {
					acc = &toolAccumulator{buf: &strings.Builder{}}
					toolAcc[i] = acc
				}
				if call.ID != "" {
					acc.id = call.ID
				}
				if call.Function.Name != "" {
					acc.name = call.Function.Name
				}
				if call.Function.Arguments != "" {
					acc.buf.WriteString(call.Function.Arguments)
				}

				args, err := parseArguments(acc.buf.String())
				if err != nil || acc.name == "" {
					continue
				}

				id := acc.id
				if id == "" {
					id = fmt.Sprintf("index_%d", i)
				}

				stream.Push(core.StreamEvent{
					Type: core.EventToolCall,
					ToolCall: core.ToolCall{
						ID:    id,
						Name:  acc.name,
						Input: args,
					},
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  delta.ID,
					Model:     delta.Model,
					Provider:  "openai",
				})

				delete(toolAcc, i)
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

func applyProviderOptions(payload *chatCompletionRequest, opts map[string]any, profile modelProfile) ([]core.Warning, error) {
	if payload == nil || len(opts) == 0 {
		return nil, nil
	}
	warnings := make([]core.Warning, 0)
	for key, value := range opts {
		switch key {
		case "response_format", "openai.response_format":
			payload.ResponseFormat = value
		case "presence_penalty", "openai.presence_penalty":
			if !profile.AllowPenalties {
				warnings = append(warnings, warnDroppedParam("presence_penalty", payload.Model, "not supported by this model"))
				continue
			}
			if f, err := toFloat32(value); err == nil {
				payload.PresencePenalty = f
			} else {
				return nil, fmt.Errorf("presence_penalty: %w", err)
			}
		case "frequency_penalty", "openai.frequency_penalty":
			if !profile.AllowPenalties {
				warnings = append(warnings, warnDroppedParam("frequency_penalty", payload.Model, "not supported by this model"))
				continue
			}
			if f, err := toFloat32(value); err == nil {
				payload.FrequencyPenalty = f
			} else {
				return nil, fmt.Errorf("frequency_penalty: %w", err)
			}
		case "logit_bias", "openai.logit_bias":
			if !profile.AllowLogitBias {
				warnings = append(warnings, warnDroppedParam("logit_bias", payload.Model, "not supported by this model"))
				continue
			}
			bias, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("logit_bias must be object")
			}
			payload.LogitBias = map[string]float32{}
			for token, raw := range bias {
				f, err := toFloat32(raw)
				if err != nil {
					return nil, fmt.Errorf("logit_bias[%s]: %w", token, err)
				}
				payload.LogitBias[token] = f
			}
		case "user", "openai.user":
			if s, ok := value.(string); ok {
				payload.User = s
			} else {
				return nil, fmt.Errorf("user must be string")
			}
		case "seed", "openai.seed":
			if !profile.AllowSeed {
				warnings = append(warnings, warnDroppedParam("seed", payload.Model, "not supported by this model"))
				continue
			}
			if i, err := toInt(value); err == nil {
				payload.Seed = &i
			} else {
				return nil, fmt.Errorf("seed: %w", err)
			}
		case "stop", "openai.stop":
			stops, err := toStringSlice(value)
			if err != nil {
				return nil, fmt.Errorf("stop: %w", err)
			}
			payload.Stop = stops
		case "reasoning_effort", "openai.reasoning_effort":
			effort := strings.ToLower(fmt.Sprint(value))
			if profile.AllowedReasoningEffort == nil {
				warnings = append(warnings, warnDroppedParam("reasoning_effort", payload.Model, "not supported by this model"))
				continue
			}
			if _, ok := profile.AllowedReasoningEffort[effort]; !ok {
				return nil, fmt.Errorf("reasoning_effort %q not supported by model %s", effort, payload.Model)
			}
			payload.ReasoningEffort = effort
		case "max_completion_tokens", "openai.max_completion_tokens":
			val, err := toInt(value)
			if err != nil {
				return nil, fmt.Errorf("max_completion_tokens: %w", err)
			}
			if profile.UseMaxCompletionTokens {
				payload.MaxCompletionTokens = val
			} else {
				payload.MaxTokens = val
			}
		case "max_output_tokens", "openai.max_output_tokens":
			val, err := toInt(value)
			if err != nil {
				return nil, fmt.Errorf("max_output_tokens: %w", err)
			}
			if profile.UseMaxCompletionTokens {
				payload.MaxCompletionTokens = val
			} else {
				payload.MaxTokens = val
			}
		default:
			// ignore unknown options for forward compatibility
		}
	}
	return warnings, nil
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

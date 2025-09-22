package openairesponses

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

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/schema"
	"go.opentelemetry.io/otel/attribute"
)

// Client implements the core.Provider interface for OpenAI's Responses API.
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

// New creates a new OpenAI Responses API client.
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

// GenerateText implements core.Provider.
func (c *Client) GenerateText(ctx context.Context, req core.Request) (_ *core.TextResult, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai-responses.GenerateText",
		attribute.String("ai.provider", "openai-responses"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		recorder.End(err, usageTokens)
	}()

	payload, warnings, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	body, err := c.doRequest(ctx, "POST", "/responses", payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp ActualResponsesResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode responses api response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("api error: %s - %s", resp.Error.Type, resp.Error.Message)
	}

	text, citations, _ := c.extractActualOutputContent(resp.Output)
	usage := c.convertActualUsage(resp.Usage)
	usageTokens = obs.UsageFromCore(usage)

	result := &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "openai-responses",
		Usage:        usage,
		Citations:    citations,
		FinishReason: core.StopReason{Type: core.StopReasonComplete},
	}
	if len(warnings) > 0 {
		result.Warnings = append(result.Warnings, warnings...)
	}
	return result, nil
}

// StreamText implements core.Provider.
func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai-responses.StreamText",
		attribute.String("ai.provider", "openai-responses"),
	)
	payload, warnings, err := c.buildPayload(req, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	body, err := c.doRequestSSE(ctx, "POST", "/responses", payload)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}

	stream := core.NewStream(ctx, 128)
	if len(warnings) > 0 {
		stream.AddWarnings(warnings...)
	}
	modelName := payload.Model
	go func() {
		c.consumeSSEStream(ctx, body, stream, modelName)
		meta := stream.Meta()
		recorder.End(stream.Err(), obs.UsageFromCore(meta.Usage))
	}()
	return stream, nil
}

// GenerateObject implements core.Provider for structured outputs.
func (c *Client) GenerateObject(ctx context.Context, req core.Request) (_ *core.ObjectResultRaw, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai-responses.GenerateObject",
		attribute.String("ai.provider", "openai-responses"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		recorder.End(err, usageTokens)
	}()
	// Add JSON schema format to request if not present
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}

	// Use inline structured output format
	payload, warnings, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	// Ensure JSON output format
	if payload.Text == nil {
		payload.Text = &TextFormatParams{}
	}
	if payload.Text.Format == nil {
		payload.Text.Format = &TextFormat{
			Type: "json_schema",
		}
	}
	payload.Text.Strict = true

	body, err := c.doRequest(ctx, "POST", "/responses", payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp ActualResponsesResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode responses api response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("api error: %s - %s", resp.Error.Type, resp.Error.Message)
	}

	// Extract JSON from output
	jsonContent := c.extractActualJSONContent(resp.Output)

	result := &core.ObjectResultRaw{
		JSON:     []byte(jsonContent),
		Model:    resp.Model,
		Provider: "openai-responses",
		Usage:    c.convertActualUsage(resp.Usage),
	}
	usageTokens = obs.UsageFromCore(result.Usage)
	if len(warnings) > 0 {
		result.Warnings = append(result.Warnings, warnings...)
	}
	return result, nil
}

// StreamObject implements core.Provider for streaming structured outputs.
func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.openai-responses.StreamObject",
		attribute.String("ai.provider", "openai-responses"),
	)
	// Add JSON format
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}

	payload, warnings, err := c.buildPayload(req, true)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}
	recorder.AddAttributes(attribute.String("ai.model", payload.Model))

	// Ensure JSON output format
	if payload.Text == nil {
		payload.Text = &TextFormatParams{}
	}
	if payload.Text.Format == nil {
		payload.Text.Format = &TextFormat{
			Type: "json_schema",
		}
	}

	body, err := c.doRequestSSE(ctx, "POST", "/responses", payload)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		return nil, err
	}

	stream := core.NewStream(ctx, 128)
	if len(warnings) > 0 {
		stream.AddWarnings(warnings...)
	}
	modelName := payload.Model
	go func() {
		c.consumeSSEStream(ctx, body, stream, modelName)
		meta := stream.Meta()
		recorder.End(stream.Err(), obs.UsageFromCore(meta.Usage))
	}()
	return core.NewObjectStreamRaw(stream), nil
}

// Capabilities implements core.Provider.
func (c *Client) Capabilities() core.Capabilities {
	return core.Capabilities{
		Streaming:         true,
		ParallelToolCalls: true,
		StrictJSON:        true,
		Images:            true,
		Audio:             true,
		Video:             false,
		Files:             true,
		Reasoning:         true,
		Citations:         true,
		Safety:            true,
		Sessions:          true, // Via previous_response_id
		MaxInputTokens:    128000,
		MaxOutputTokens:   16384,
		Provider:          "openai-responses",
		Models: []string{
			"gpt-4o-mini", "gpt-4o", "gpt-4.1",
			"o3", "o4-mini", "gpt-5",
		},
	}
}

// buildPayload converts a core.Request to a ResponsesRequest.
func (c *Client) buildPayload(req core.Request, stream bool) (*ResponsesRequest, []core.Warning, error) {
	// Convert messages to input format
	input, instructions := c.convertMessages(req.Messages)

	payload := &ResponsesRequest{
		Model:        c.chooseModel(req.Model),
		Input:        input,
		Instructions: instructions,
		Stream:       stream,
		Store:        c.opts.store,
	}
	policy := policyForModel(payload.Model)
	warnings := make([]core.Warning, 0, 2)

	// Apply default instructions if none provided
	if payload.Instructions == "" && c.opts.defaultInstructions != "" {
		payload.Instructions = c.opts.defaultInstructions
	}

	// Handle provider-specific options
	if req.ProviderOptions != nil {
		optionWarnings, err := c.applyProviderOptions(payload, req.ProviderOptions, policy)
		if err != nil {
			return nil, nil, err
		}
		if len(optionWarnings) > 0 {
			warnings = append(warnings, optionWarnings...)
		}
	}

	// Convert tools if present
	if len(req.Tools) > 0 {
		payload.Tools = c.convertTools(req.Tools)
	}

	// Apply temperature and other generation parameters
	if req.Temperature > 0 {
		if policy.AllowTemperature {
			payload.Temperature = req.Temperature
		} else {
			warnings = append(warnings, warnDroppedParam("temperature", payload.Model, "not supported"))
		}
	}
	if req.MaxTokens > 0 {
		if policy.UseMaxOutputTokens {
			payload.MaxOutputTokens = req.MaxTokens
		} else {
			payload.MaxOutputTokens = req.MaxTokens
		}
	}
	if req.TopP > 0 {
		if policy.AllowTopP {
			payload.TopP = req.TopP
		} else {
			warnings = append(warnings, warnDroppedParam("top_p", payload.Model, "not supported"))
		}
	}

	// Apply default reasoning if configured
	if c.opts.defaultReasoning != nil && payload.Reasoning == nil && policy.AllowReasoning {
		if policy.AllowedReasoningEffort != nil {
			if _, ok := policy.AllowedReasoningEffort[strings.ToLower(c.opts.defaultReasoning.Effort)]; !ok && c.opts.defaultReasoning.Effort != "" {
				warnings = append(warnings, warnDroppedParam("reasoning.effort", payload.Model, "unsupported default value"))
			} else {
				payload.Reasoning = c.opts.defaultReasoning
			}
		} else {
			payload.Reasoning = c.opts.defaultReasoning
		}
	}

	return payload, warnings, nil
}

// convertMessages converts core messages to Responses API input format.
func (c *Client) convertMessages(messages []core.Message) (any, string) {
	if len(messages) == 0 {
		return "", ""
	}

	var instructions string
	var inputMessages []ResponseInputParam

	for _, msg := range messages {
		// Extract system/developer instructions
		if msg.Role == core.System {
			for _, part := range msg.Parts {
				if text, ok := part.(core.Text); ok {
					if instructions != "" {
						instructions += "\n"
					}
					instructions += text.Text
				}
			}
			continue
		}

		// Convert role
		role := string(msg.Role)
		if msg.Role == core.System {
			role = "developer"
		}

		// Convert parts
		var content []ContentPart
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case core.Text:
				content = append(content, ContentPart{
					Type: "input_text",
					Text: p.Text,
				})
			case core.Image:
				// Convert BlobRef to appropriate format
				if p.Source.Kind == core.BlobBytes {
					dataURL := fmt.Sprintf("data:%s;base64,%s",
						p.Source.MIME,
						base64.StdEncoding.EncodeToString(p.Source.Bytes))
					content = append(content, ContentPart{
						Type:     "input_image",
						ImageURL: dataURL,
					})
				} else if p.Source.Kind == core.BlobURL {
					content = append(content, ContentPart{
						Type:     "input_image",
						ImageURL: p.Source.URL,
					})
				}
			case core.ImageURL:
				content = append(content, ContentPart{
					Type:     "input_image",
					ImageURL: p.URL,
					Detail:   p.Detail,
				})
			case core.Audio:
				if p.Source.Kind == core.BlobBytes {
					content = append(content, ContentPart{
						Type: "input_audio",
						InputAudio: &InputAudioData{
							Data:   base64.StdEncoding.EncodeToString(p.Source.Bytes),
							Format: p.Format,
						},
					})
				}
			case core.File:
				if p.Source.Kind == core.BlobProvider {
					content = append(content, ContentPart{
						Type:   "input_file",
						FileID: p.Source.ProviderID,
					})
				}
			case core.ToolResult:
				// Tool results become tool role messages
				role = "tool"
				content = append(content, ContentPart{
					Type: "output_text",
					Text: fmt.Sprintf("%v", p.Result),
				})
			}
		}

		if len(content) > 0 {
			inputMessages = append(inputMessages, ResponseInputParam{
				Role:    role,
				Content: content,
			})
		}
	}

	// If only one message and it's simple text, return as string
	if len(inputMessages) == 1 && len(inputMessages[0].Content) == 1 {
		if inputMessages[0].Content[0].Type == "input_text" {
			return inputMessages[0].Content[0].Text, instructions
		}
	}

	return inputMessages, instructions
}

// convertTools converts core tool handles to Responses API tools.
func (c *Client) convertTools(tools []core.ToolHandle) []ResponseTool {
	var result []ResponseTool

	for _, tool := range tools {
		// Convert to function tool
		params := make(map[string]any)
		if schema := tool.InputSchema(); schema != nil {
			// Convert our schema format to OpenAI's expected format
			params = schemaToMap(schema)
		}

		result = append(result, ResponseTool{
			Type:        "function",
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  params,
		})
	}

	return result
}

// applyProviderOptions applies provider-specific options to the payload.
func (c *Client) applyProviderOptions(payload *ResponsesRequest, options map[string]any, policy modelPolicy) ([]core.Warning, error) {
	warnings := make([]core.Warning, 0)
	for key, value := range options {
		switch key {
		case "openai-responses.previous_response_id":
			if id, ok := value.(string); ok {
				payload.PreviousResponseID = id
			}
		case "openai-responses.background":
			if bg, ok := value.(bool); ok {
				payload.Background = bg
			}
		case "openai-responses.reasoning":
			if reasoning, ok := value.(map[string]any); ok {
				if !policy.AllowReasoning {
					warnings = append(warnings, warnDroppedParam("reasoning", payload.Model, "not supported"))
					continue
				}
				reasoningParams := &ReasoningParams{}
				if effort, ok := reasoning["effort"].(string); ok {
					lower := strings.ToLower(effort)
					if policy.AllowedReasoningEffort != nil {
						if _, ok := policy.AllowedReasoningEffort[lower]; !ok && lower != "" {
							warnings = append(warnings, warnDroppedParam("reasoning.effort", payload.Model, "unsupported value"))
						} else {
							reasoningParams.Effort = lower
						}
					} else {
						reasoningParams.Effort = lower
					}
				}
				if summary, ok := reasoning["summary"].(string); ok {
					reasoningParams.Summary = &summary
				}
				payload.Reasoning = reasoningParams
			}
		case "openai-responses.hosted_tools":
			if tools, ok := value.([]ResponseTool); ok {
				payload.Tools = append(payload.Tools, tools...)
			}
		case "openai-responses.store":
			if store, ok := value.(bool); ok {
				payload.Store = &store
			}
		case "openai-responses.metadata":
			if meta, ok := value.(map[string]any); ok {
				payload.Metadata = meta
			}
		case "openai-responses.verbosity":
			if val, ok := value.(string); ok {
				if !policy.AllowVerbosity {
					warnings = append(warnings, warnDroppedParam("verbosity", payload.Model, "not supported"))
					continue
				}
				lower := strings.ToLower(val)
				if policy.VerbosityOptions != nil {
					if _, ok := policy.VerbosityOptions[lower]; !ok {
						warnings = append(warnings, warnDroppedParam("verbosity", payload.Model, "unsupported value"))
						continue
					}
				}
				if payload.Text == nil {
					payload.Text = &TextFormatParams{}
				}
				payload.Text.Verbosity = lower
			}
		case "openai-responses.temperature":
			if temp, ok := value.(float64); ok {
				if policy.AllowTemperature {
					payload.Temperature = float32(temp)
				} else {
					warnings = append(warnings, warnDroppedParam("temperature", payload.Model, "not supported"))
				}
			}
		case "openai-responses.max_output_tokens":
			if val, err := toInt(value); err == nil {
				payload.MaxOutputTokens = val
			}
		}
	}
	return warnings, nil
}

// doRequest makes an HTTP request to the API.
func (c *Client) doRequest(ctx context.Context, method, path string, payload any) (io.ReadCloser, error) {
	url := strings.TrimRight(c.opts.baseURL, "/") + path

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.opts.apiKey)
	for k, v := range c.opts.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var errorResp struct {
			Error ResponseError `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("status %d: failed to decode error", resp.StatusCode)
		}
		return nil, c.wrapError(errorResp.Error, resp.StatusCode)
	}

	return resp.Body, nil
}

// doRequestSSE makes an SSE request to the API.
func (c *Client) doRequestSSE(ctx context.Context, method, path string, payload any) (io.ReadCloser, error) {
	url := strings.TrimRight(c.opts.baseURL, "/") + path

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Set headers for SSE
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.opts.apiKey)
	for k, v := range c.opts.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var errorResp struct {
			Error ResponseError `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("status %d: failed to decode error", resp.StatusCode)
		}
		return nil, c.wrapError(errorResp.Error, resp.StatusCode)
	}

	return resp.Body, nil
}

// consumeSSEStream processes the SSE stream and emits normalized events.
func (c *Client) consumeSSEStream(ctx context.Context, body io.ReadCloser, stream *core.Stream, modelName string) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*64), 1024*64) // 64KB buffer

	var currentEvent string
	var textBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var usage core.Usage
	var responseID string

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "event: <type>" or "data: <json>"
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Skip [DONE] marker
			if data == "[DONE]" {
				continue
			}

			// Process based on event type
			switch currentEvent {
			case "response.created":
				var evt StreamEventResponseCreated
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					responseID = evt.ID
					stream.Push(core.StreamEvent{
						Type:     core.EventStart,
						StreamID: responseID,
					})
				}

			case "response.output_text.delta":
				var evt StreamEventTextDelta
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					textBuilder.WriteString(evt.Delta)
					stream.Push(core.StreamEvent{
						Type:      core.EventTextDelta,
						TextDelta: evt.Delta,
						StreamID:  responseID,
					})
				}

			case "response.output_text.done":
				// Final text event - nothing special to do

			case "response.reasoning_text.delta":
				var evt StreamEventReasoningDelta
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					reasoningBuilder.WriteString(evt.Delta)
					stream.Push(core.StreamEvent{
						Type:           core.EventReasoningDelta,
						ReasoningDelta: evt.Delta,
						StreamID:       responseID,
					})
				}

			case "response.reasoning_text.done":
				var evt StreamEventReasoningDone
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					if evt.Summary != "" {
						stream.Push(core.StreamEvent{
							Type:             core.EventReasoningSummary,
							ReasoningSummary: evt.Summary,
							StreamID:         responseID,
						})
					}
				}

			case "response.tool_call":
				var evt StreamEventToolCall
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					stream.Push(core.StreamEvent{
						Type: core.EventToolCall,
						ToolCall: core.ToolCall{
							ID:    evt.ID,
							Name:  evt.Name,
							Input: evt.Arguments,
						},
						StreamID: responseID,
					})
				}

			case "response.completed":
				var evt StreamEventCompleted
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					if evt.Usage != nil {
						usage = c.convertActualUsage(evt.Usage)
					}
					stream.Push(core.StreamEvent{
						Type:     core.EventFinish,
						Usage:    usage,
						StreamID: responseID,
						FinishReason: &core.StopReason{
							Type: core.StopReasonComplete,
						},
					})
				}

			case "response.error":
				var evt StreamEventError
				if err := json.Unmarshal([]byte(data), &evt); err == nil {
					// Error event - fail the stream
					stream.Fail(errors.New(evt.Message))
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		stream.Fail(err)
		return
	}

	// Set final metadata
	stream.SetMeta(core.StreamMeta{
		Model:    modelName,
		Provider: "openai-responses",
		Usage:    usage,
	})
}

// Helper methods

func (c *Client) chooseModel(requested string) string {
	if requested != "" {
		return requested
	}
	return c.opts.model
}

func (c *Client) extractActualOutputContent(items []ActualResponseItem) (string, []core.Citation, []core.ToolCall) {
	var textBuilder strings.Builder
	var citations []core.Citation
	var toolCalls []core.ToolCall

	for _, item := range items {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					textBuilder.WriteString(part.Text)
					// Annotations would need parsing from json.RawMessage if needed
				}
			}

		case "tool_call":
			toolCalls = append(toolCalls, core.ToolCall{
				ID:    item.ID,
				Name:  item.Name,
				Input: item.Arguments,
			})
		}
	}

	return textBuilder.String(), citations, toolCalls
}

func (c *Client) extractActualJSONContent(items []ActualResponseItem) string {
	for _, item := range items {
		if item.Type == "message" {
			for _, part := range item.Content {
				if part.Type == "output_text" {
					return part.Text
				}
			}
		}
	}
	return "{}"
}

func (c *Client) convertActualUsage(usage *ActualUsage) core.Usage {
	if usage == nil {
		return core.Usage{}
	}
	result := core.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.InputTokensDetails != nil {
		result.CachedInputTokens = usage.InputTokensDetails.CachedTokens
	}
	if usage.OutputTokensDetails != nil {
		result.ReasoningTokens = usage.OutputTokensDetails.ReasoningTokens
	}
	return result
}

func (c *Client) wrapError(apiErr ResponseError, statusCode int) error {
	var code core.ErrorCode
	retryable := false

	switch statusCode {
	case 429:
		code = core.ErrRateLimited
		retryable = true
	case 400:
		code = core.ErrBadRequest
	case 500, 502, 503, 504:
		code = core.ErrTransient
		retryable = true
	default:
		code = core.ErrProviderError
	}

	return &core.AIError{
		Code:      code,
		Message:   apiErr.Message,
		Status:    statusCode,
		Retryable: retryable,
	}
}

// Helper function
func schemaToMap(s *schema.Schema) map[string]any {
	// Convert our schema format to OpenAI's expected format
	result := make(map[string]any)

	if s.Type != "" {
		result["type"] = s.Type
	}
	if s.Description != "" {
		result["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		result["properties"] = props
	}
	if len(s.Required) > 0 {
		result["required"] = s.Required
	}
	if s.Items != nil {
		result["items"] = schemaToMap(s.Items)
	}

	return result
}

func toInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case json.Number:
		i, err := v.Int64()
		return int(i), err
	case string:
		var num json.Number = json.Number(v)
		i, err := num.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("invalid number type %T", value)
	}
}

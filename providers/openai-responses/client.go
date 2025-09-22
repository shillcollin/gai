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
	"github.com/shillcollin/gai/schema"
)

// Client implements the core.Provider interface for OpenAI's Responses API.
type Client struct {
	httpClient *http.Client
	opts       options
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
func (c *Client) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

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

	// Extract text from output items
	text, citations, _ := c.extractActualOutputContent(resp.Output)

	// Convert usage
	usage := c.convertActualUsage(resp.Usage)

	// Determine finish reason
	finishReason := core.StopReason{
		Type: core.StopReasonComplete,
	}

	return &core.TextResult{
		Text:         text,
		Model:        resp.Model,
		Provider:     "openai-responses",
		Usage:        usage,
		Citations:    citations,
		FinishReason: finishReason,
	}, nil
}

// StreamText implements core.Provider.
func (c *Client) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	payload, err := c.buildPayload(req, true)
	if err != nil {
		return nil, err
	}

	body, err := c.doRequestSSE(ctx, "POST", "/responses", payload)
	if err != nil {
		return nil, err
	}

	stream := core.NewStream(ctx, 128)
	go c.consumeSSEStream(ctx, body, stream)
	return stream, nil
}

// GenerateObject implements core.Provider for structured outputs.
func (c *Client) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	// Add JSON schema format to request if not present
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}

	// Use inline structured output format
	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

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

	return &core.ObjectResultRaw{
		JSON:     []byte(jsonContent),
		Model:    resp.Model,
		Provider: "openai-responses",
		Usage:    c.convertActualUsage(resp.Usage),
	}, nil
}

// StreamObject implements core.Provider for streaming structured outputs.
func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	// Add JSON format
	if req.ProviderOptions == nil {
		req.ProviderOptions = map[string]any{}
	}

	payload, err := c.buildPayload(req, true)
	if err != nil {
		return nil, err
	}

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
		return nil, err
	}

	stream := core.NewStream(ctx, 128)
	go c.consumeSSEStream(ctx, body, stream)
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
func (c *Client) buildPayload(req core.Request, stream bool) (*ResponsesRequest, error) {
	// Convert messages to input format
	input, instructions := c.convertMessages(req.Messages)

	payload := &ResponsesRequest{
		Model:        c.chooseModel(req.Model),
		Input:        input,
		Instructions: instructions,
		Stream:       stream,
		Store:        c.opts.store,
	}

	// Apply default instructions if none provided
	if payload.Instructions == "" && c.opts.defaultInstructions != "" {
		payload.Instructions = c.opts.defaultInstructions
	}

	// Handle provider-specific options
	if req.ProviderOptions != nil {
		c.applyProviderOptions(payload, req.ProviderOptions)
	}

	// Convert tools if present
	if len(req.Tools) > 0 {
		payload.Tools = c.convertTools(req.Tools)
	}

	// Apply temperature and other generation parameters
	if req.Temperature > 0 {
		payload.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload.MaxOutputTokens = req.MaxTokens
	}
	if req.TopP > 0 {
		payload.TopP = req.TopP
	}

	// Apply default reasoning if configured
	if c.opts.defaultReasoning != nil && payload.Reasoning == nil {
		payload.Reasoning = c.opts.defaultReasoning
	}

	return payload, nil
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
func (c *Client) applyProviderOptions(payload *ResponsesRequest, options map[string]any) {
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
				payload.Reasoning = &ReasoningParams{}
				if effort, ok := reasoning["effort"].(string); ok {
					payload.Reasoning.Effort = effort
				}
				if summary, ok := reasoning["summary"].(string); ok {
					payload.Reasoning.Summary = &summary
				}
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
		}
	}
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
func (c *Client) consumeSSEStream(ctx context.Context, body io.ReadCloser, stream *core.Stream) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*64), 1024*64) // 64KB buffer

	var currentEvent string
	var textBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var usage core.Usage
	var responseID string
	var model string

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
				// Reasoning summary event - would need to be added to core.StreamEvent if needed

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
		Model:    model,
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
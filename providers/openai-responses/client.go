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
	"os"
	"strconv"
	"strings"
	"time"

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

type pendingToolCall struct {
	CallID  string
	Name    string
	builder strings.Builder
}

func (p *pendingToolCall) appendDelta(delta string) {
	p.builder.WriteString(delta)
}

func (p *pendingToolCall) setArguments(args string) {
	p.builder.Reset()
	p.builder.WriteString(args)
}

func (p *pendingToolCall) arguments() map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(p.builder.String()), &args); err != nil || args == nil {
		return map[string]any{"raw": p.builder.String()}
	}
	return args
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
			"o3", "o4-mini", "gpt-5", "gpt-5-codex",
		},
	}
}

// buildPayload converts a core.Request to a ResponsesRequest.
func (c *Client) buildPayload(req core.Request, stream bool) (*ResponsesRequest, []core.Warning, error) {
	// Convert messages to input format
	inputs, instructions, err := c.convertMessages(req.Messages)
	if err != nil {
		return nil, nil, err
	}

	payload := &ResponsesRequest{
		Model:        c.chooseModel(req.Model),
		Input:        inputs,
		Instructions: instructions,
		Stream:       stream,
		Store:        c.opts.store,
	}

	if len(inputs) == 0 {
		payload.Input = []any{}
	}
	policy := policyForModel(payload.Model)
	warnings := make([]core.Warning, 0, 2)

	// Apply default instructions if none provided
	if payload.Instructions == "" && c.opts.defaultInstructions != "" {
		payload.Instructions = c.opts.defaultInstructions
	}

	if req.Metadata != nil && len(req.Metadata) > 0 {
		payload.Metadata = req.Metadata
	}

	if req.Session != nil && req.Session.ID != "" && payload.PreviousResponseID == "" {
		payload.PreviousResponseID = req.Session.ID
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
		switch req.ToolChoice {
		case core.ToolChoiceRequired:
			payload.ToolChoice = "required"
		case core.ToolChoiceNone:
			payload.ToolChoice = "none"
		default:
			payload.ToolChoice = "auto"
		}
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

	if req.TopK > 0 {
		payload.TopK = req.TopK
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

	if os.Getenv("GAI_DEBUG_PAYLOAD") == "1" {
		if data, err := json.MarshalIndent(payload, "", "  "); err == nil {
			fmt.Fprintf(os.Stderr, "[GAI] OpenAI Responses payload:\n%s\n", string(data))
		}
	}

	return payload, warnings, nil
}

// convertMessages converts core messages to Responses API input format.
func (c *Client) convertMessages(messages []core.Message) ([]any, string, error) {
	if len(messages) == 0 {
		return nil, "", nil
	}

	var (
		instructions string
		inputs       []any
	)

	appendMessage := func(role string, parts []ContentPart) {
		if len(parts) == 0 {
			return
		}
		inputs = append(inputs, ResponseInputParam{Role: role, Content: parts})
	}

	for _, msg := range messages {
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

		role := string(msg.Role)
		switch msg.Role {
		case core.System:
			role = "developer"
		case core.Assistant:
			role = "assistant"
		case core.User:
			role = "user"
		}

		contentType := "input_text"
		if msg.Role == core.Assistant {
			contentType = "output_text"
		}

		var messageParts []ContentPart

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case core.Text:
				if trimmed := strings.TrimSpace(p.Text); trimmed != "" {
					messageParts = append(messageParts, ContentPart{Type: contentType, Text: p.Text})
				}
			case core.Image:
				switch p.Source.Kind {
				case core.BlobBytes:
					dataURL := fmt.Sprintf("data:%s;base64,%s", p.Source.MIME, base64.StdEncoding.EncodeToString(p.Source.Bytes))
					messageParts = append(messageParts, ContentPart{Type: "input_image", ImageURL: dataURL})
				case core.BlobPath:
					data, err := p.Source.Read()
					if err != nil {
						return nil, "", fmt.Errorf("read image from path %s: %w", p.Source.Path, err)
					}
					dataURL := fmt.Sprintf("data:%s;base64,%s", p.Source.MIME, base64.StdEncoding.EncodeToString(data))
					messageParts = append(messageParts, ContentPart{Type: "input_image", ImageURL: dataURL})
				case core.BlobURL:
					messageParts = append(messageParts, ContentPart{Type: "input_image", ImageURL: p.Source.URL})
				case core.BlobProvider:
					return nil, "", fmt.Errorf("image blob provider references are not supported; upload file and pass file_id instead")
				}
			case core.ImageURL:
				messageParts = append(messageParts, ContentPart{Type: "input_image", ImageURL: p.URL, Detail: p.Detail})
			case core.Audio:
				if p.Source.Kind == core.BlobBytes {
					messageParts = append(messageParts, ContentPart{
						Type:       "input_audio",
						InputAudio: &InputAudioData{Data: base64.StdEncoding.EncodeToString(p.Source.Bytes), Format: p.Format},
					})
				}
			case core.File:
				if p.Source.Kind == core.BlobProvider {
					messageParts = append(messageParts, ContentPart{Type: "input_file", FileID: p.Source.ProviderID})
				}
			case core.ToolCall:
				// Preserve tool call history so the model can reference previous invocations.
				inputs = append(inputs, FunctionCallParam{
					Type:      "function_call",
					CallID:    p.ID,
					Name:      p.Name,
					Arguments: mustJSONString(p.Input),
				})
			case core.ToolResult:
				output := ""
				if str, ok := p.Result.(string); ok {
					output = str
				} else if p.Result != nil {
					output = mustJSONString(p.Result)
				}
				if output == "" && p.Error != "" {
					output = p.Error
				}
				if output == "" {
					continue
				}
				inputs = append(inputs, FunctionCallOutputParam{
					Type:   "function_call_output",
					CallID: p.ID,
					Output: output,
				})
			default:
				continue
			}
		}

		appendMessage(role, messageParts)
	}

	return inputs, instructions, nil
}

func mustJSONString(v any) string {
	if v == nil {
		return "null"
	}
	if data, err := json.Marshal(v); err == nil {
		return string(data)
	}
	return fmt.Sprintf("%v", v)
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
		case "openai-responses.modalities":
			payload.Modalities = toStringSlice(value)
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
		case "openai-responses.max_prompt_tokens":
			if val, err := toInt(value); err == nil {
				payload.MaxPromptTokens = val
			}
		case "openai-responses.max_tool_calls":
			if val, err := toInt(value); err == nil {
				payload.MaxToolCalls = val
			}
		case "openai-responses.parallel_tool_calls":
			if b, ok := value.(bool); ok {
				payload.ParallelToolCalls = &b
			}
		case "openai-responses.parallel_tool_call_limit":
			if val, err := toInt(value); err == nil {
				payload.ParallelToolLimit = val
			}
		case "openai-responses.tool_choice":
			payload.ToolChoice = value
		case "openai-responses.user":
			if val, ok := value.(string); ok {
				payload.User = val
			}
		case "openai-responses.seed":
			if val, err := toInt(value); err == nil {
				payload.Seed = &val
			}
		case "openai-responses.truncation":
			if val, ok := value.(string); ok {
				payload.Truncation = val
			}
		case "openai-responses.prediction":
			if val, ok := value.(map[string]any); ok {
				payload.Prediction = val
			}
		case "openai-responses.stop":
			payload.Stop = toStringSlice(value)
		case "openai-responses.text":
			if params := toTextFormatParams(value); params != nil {
				payload.Text = params
			}
		case "openai-responses.audio":
			if params := toAudioParams(value); params != nil {
				payload.Audio = params
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
func (c *Client) consumeSSEStream(ctx context.Context, body io.ReadCloser, stream *core.Stream, defaultModel string) {
	defer body.Close()
	defer stream.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*64), 1024*1024)

	debug := os.Getenv("GAI_DEBUG_SSE") == "1"

	responseID := ""
	modelName := defaultModel
	lastUsage := core.Usage{}
	started := false
	pendingCalls := make(map[string]*pendingToolCall)

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if debug {
			fmt.Fprintf(os.Stderr, "[GAI] SSE %s: %s\n", currentEvent, data)
		}
		if data == "[DONE]" {
			continue
		}

		var envelope struct {
			Type     string `json:"type"`
			Sequence int    `json:"sequence_number"`
		}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			stream.Fail(err)
			continue
		}

		seq := envelope.Sequence

		switch envelope.Type {
		case "response.created", "response.in_progress":
			var evt StreamEventResponseCreated
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			responseID = evt.Response.ID
			if evt.Response.Model != "" {
				modelName = evt.Response.Model
			}
			if !started {
				stream.Push(core.StreamEvent{
					Type:      core.EventStart,
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  responseID,
					Provider:  "openai-responses",
					Model:     modelName,
				})
				started = true
			}
		case "response.output_item.added":
			var evt StreamEventOutputItemAdded
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if evt.Item.Type == "function_call" {
				pendingCalls[evt.Item.ID] = &pendingToolCall{CallID: evt.Item.CallID, Name: evt.Item.Name}
			}
		case "response.output_text.delta":
			var evt StreamEventTextDelta
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			ext := map[string]any{}
			if len(evt.Logprobs) > 0 {
				ext["logprobs"] = evt.Logprobs
			}
			if evt.Obfuscation != "" {
				ext["obfuscation"] = evt.Obfuscation
			}
			stream.Push(core.StreamEvent{
				Type:      core.EventTextDelta,
				TextDelta: evt.Delta,
				Timestamp: time.Now(),
				Schema:    "gai.events.v1",
				Seq:       seq,
				StreamID:  evt.ItemID,
				Provider:  "openai-responses",
				Model:     modelName,
				Ext:       extOrNil(ext),
			})
		case "response.output_text.done":
			// no-op; the deltas already emitted the content
		case "response.reasoning_text.delta":
			var evt StreamEventReasoningDelta
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			stream.Push(core.StreamEvent{
				Type:           core.EventReasoningDelta,
				ReasoningDelta: evt.Delta,
				Timestamp:      time.Now(),
				Schema:         "gai.events.v1",
				Seq:            seq,
				StreamID:       evt.ItemID,
				Provider:       "openai-responses",
				Model:          modelName,
			})
		case "response.reasoning_text.done":
			var evt StreamEventReasoningDone
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if evt.Summary != "" {
				stream.Push(core.StreamEvent{
					Type:             core.EventReasoningSummary,
					ReasoningSummary: evt.Summary,
					Timestamp:        time.Now(),
					Schema:           "gai.events.v1",
					Seq:              seq,
					StreamID:         evt.ItemID,
					Provider:         "openai-responses",
					Model:            modelName,
				})
			}
		case "response.content_part.done":
			var evt StreamEventContentPart
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if citations := annotationsToCitations(evt.Part.Annotations); len(citations) > 0 {
				stream.Push(core.StreamEvent{
					Type:      core.EventCitations,
					Citations: citations,
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  evt.ItemID,
					Provider:  "openai-responses",
					Model:     modelName,
				})
			}
		case "response.output_audio.delta":
			var evt StreamEventOutputAudioDelta
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			ext := map[string]any{
				"audio_b64": evt.Delta.Audio.Data,
			}
			if evt.Delta.Audio.Format != "" {
				ext["format"] = evt.Delta.Audio.Format
			}
			stream.Push(core.StreamEvent{
				Type:      core.EventAudioDelta,
				Timestamp: time.Now(),
				Schema:    "gai.events.v1",
				Seq:       seq,
				StreamID:  evt.ItemID,
				Provider:  "openai-responses",
				Model:     modelName,
				Ext:       ext,
			})
		case "response.function_call_arguments.delta", "response.output_tool_call.arguments.delta":
			var evt StreamEventFunctionCallArgumentsDelta
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if call, ok := pendingCalls[evt.ItemID]; ok {
				call.appendDelta(evt.Delta)
			}
		case "response.function_call_arguments.done", "response.output_tool_call.arguments.done":
			var evt StreamEventFunctionCallArgumentsDone
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if call, ok := pendingCalls[evt.ItemID]; ok {
				if evt.Arguments != "" {
					call.setArguments(evt.Arguments)
				}
				args := call.arguments()
				id := call.CallID
				if id == "" {
					id = evt.ItemID
				}
				stream.Push(core.StreamEvent{
					Type: core.EventToolCall,
					ToolCall: core.ToolCall{
						ID:    id,
						Name:  call.Name,
						Input: args,
					},
					Timestamp: time.Now(),
					Schema:    "gai.events.v1",
					Seq:       seq,
					StreamID:  evt.ItemID,
					Provider:  "openai-responses",
					Model:     modelName,
				})
				delete(pendingCalls, evt.ItemID)
			}
		case "response.completed":
			var evt StreamEventCompleted
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			if evt.Response.Model != "" {
				modelName = evt.Response.Model
			}
			if evt.Response.Usage != nil {
				lastUsage = c.convertActualUsage(evt.Response.Usage)
			}
			finish := core.StopReason{Type: core.StopReasonComplete}
			if evt.Response.Status != "" && evt.Response.Status != "completed" {
				finish.Type = evt.Response.Status
			}
			stream.Push(core.StreamEvent{
				Type:         core.EventFinish,
				Timestamp:    time.Now(),
				Schema:       "gai.events.v1",
				Seq:          seq,
				StreamID:     responseID,
				Provider:     "openai-responses",
				Model:        modelName,
				Usage:        lastUsage,
				FinishReason: &finish,
			})
		case "response.completed_with_error":
			var evt StreamEventCompleted
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			errMsg := "openai responses error"
			if evt.Response.Error != nil && evt.Response.Error.Message != "" {
				errMsg = evt.Response.Error.Message
			}
			stream.Fail(errors.New(errMsg))
			return
		case "response.error":
			var evt StreamEventError
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				stream.Fail(err)
				continue
			}
			stream.Fail(errors.New(evt.Message))
			return
		case "response.warning":
			var payload map[string]any
			if err := json.Unmarshal([]byte(data), &payload); err == nil {
				if msg, ok := toString(payload["message"]); ok && msg != "" {
					stream.AddWarnings(core.Warning{Code: "openai_responses_warning", Message: msg})
				}
			}
		case "response.usage.delta":
			var payload struct {
				Usage *ActualUsage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err == nil && payload.Usage != nil {
				lastUsage = c.convertActualUsage(payload.Usage)
				stream.SetMeta(core.StreamMeta{Model: modelName, Provider: "openai-responses", Usage: lastUsage})
			}
		default:
			// Unknown events are ignored but logged in debug mode.
		}
	}

	if err := scanner.Err(); err != nil {
		stream.Fail(err)
		return
	}

	stream.SetMeta(core.StreamMeta{Model: modelName, Provider: "openai-responses", Usage: lastUsage})
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
					if cites := annotationsToCitations(part.Annotations); len(cites) > 0 {
						citations = append(citations, cites...)
					}
				}
			}

		case "tool_call":
			toolCalls = append(toolCalls, core.ToolCall{
				ID:    item.ID,
				Name:  item.Name,
				Input: parseToolArguments(item.Arguments),
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

func parseToolArguments(raw json.RawMessage) map[string]any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err == nil && obj != nil {
		return obj
	}
	var str string
	if err := json.Unmarshal(trimmed, &str); err == nil {
		if err := json.Unmarshal([]byte(str), &obj); err == nil && obj != nil {
			return obj
		}
		if str != "" {
			return map[string]any{"raw": str}
		}
	}
	return map[string]any{"raw": string(trimmed)}
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

func extOrNil(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}

func annotationsToCitations(raw []json.RawMessage) []core.Citation {
	if len(raw) == 0 {
		return nil
	}
	citations := make([]core.Citation, 0, len(raw))
	for _, annRaw := range raw {
		var ann map[string]any
		if err := json.Unmarshal(annRaw, &ann); err != nil {
			continue
		}
		typeVal, _ := toString(ann["type"])
		if typeVal != "citation" {
			continue
		}
		cite := core.Citation{}
		if uri, ok := toString(ann["uri"]); ok && uri != "" {
			cite.URI = uri
		} else if uri, ok := toString(ann["url"]); ok && uri != "" {
			cite.URI = uri
		}
		if title, ok := toString(ann["title"]); ok {
			cite.Title = title
		}
		if snippet, ok := toString(ann["snippet"]); ok {
			cite.Snippet = snippet
		}
		if start, ok := toIntFromAny(ann["start_index"]); ok {
			cite.Start = start
		} else if start, ok := toIntFromAny(ann["start"]); ok {
			cite.Start = start
		}
		if end, ok := toIntFromAny(ann["end_index"]); ok {
			cite.End = end
		} else if end, ok := toIntFromAny(ann["end"]); ok {
			cite.End = end
		}
		if score, ok := toFloatFromAny(ann["score"]); ok {
			cite.Score = float32(score)
		}
		citations = append(citations, cite)
	}
	return citations
}

func toIntFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if v == "" {
			return 0, false
		}
		if i, err := strconv.Atoi(v); err == nil {
			return i, true
		}
	}
	return 0, false
}

func toFloatFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	case string:
		if v == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func toStringSlice(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := toString(item); ok {
				out = append(out, str)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		if str, ok := toString(v); ok && str != "" {
			return []string{str}
		}
	}
	return nil
}

func toString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	default:
		return "", false
	}
}

func toAudioParams(value any) *AudioParams {
	switch v := value.(type) {
	case nil:
		return nil
	case AudioParams:
		cp := v
		return &cp
	case *AudioParams:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	case map[string]any:
		params := &AudioParams{}
		if voice, ok := toString(v["voice"]); ok {
			params.Voice = voice
		}
		if format, ok := toString(v["format"]); ok {
			params.Format = format
		}
		if lang, ok := toString(v["language"]); ok {
			params.Language = lang
		}
		if quality, ok := toString(v["quality"]); ok {
			params.Quality = quality
		}
		if sr, err := toInt(v["sample_rate"]); err == nil && sr > 0 {
			params.SampleRate = sr
		}
		return params
	default:
		return nil
	}
}

func toTextFormatParams(value any) *TextFormatParams {
	switch v := value.(type) {
	case nil:
		return nil
	case TextFormatParams:
		cp := v
		return &cp
	case *TextFormatParams:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	case map[string]any:
		params := &TextFormatParams{}
		if strict, ok := v["strict"].(bool); ok {
			params.Strict = strict
		}
		if stop := toStringSlice(v["stop"]); len(stop) > 0 {
			params.Stop = stop
		}
		if verbosity, ok := toString(v["verbosity"]); ok {
			params.Verbosity = strings.ToLower(verbosity)
		}
		if formatVal, ok := v["format"]; ok {
			switch fmtVal := formatVal.(type) {
			case string:
				params.Format = &TextFormat{Type: fmtVal}
			case map[string]any:
				tf := &TextFormat{}
				if t, ok := toString(fmtVal["type"]); ok {
					tf.Type = t
				}
				if schemaVal, ok := fmtVal["json_schema"].(map[string]any); ok {
					tf.JSONSchema = &JSONSchema{}
					if name, ok := toString(schemaVal["name"]); ok {
						tf.JSONSchema.Name = name
					}
					if raw, ok := schemaVal["schema"].(map[string]any); ok {
						tf.JSONSchema.Schema = raw
					}
					if strict, ok := schemaVal["strict"].(bool); ok {
						tf.JSONSchema.Strict = strict
					}
				}
				params.Format = tf
			}
		}
		return params
	default:
		return nil
	}
}

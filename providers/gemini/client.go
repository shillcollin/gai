package gemini

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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/httpclient"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/schema"
	"go.opentelemetry.io/otel/attribute"
)

type Client struct {
	opts       options
	httpClient *http.Client
}

const (
	metadataKeyRawContent        = "gemini.raw_content"
	metadataKeyThoughtSignature  = "gemini.thought_signature"
	metadataKeyThought           = "gemini.thought"
	providerOptionThinkingBudget = "gemini.thinking.budget"
	providerOptionIncludeThought = "gemini.thinking.include_thoughts"
	providerOptionResponseMIME   = "gemini.response_mime_type"
)

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
	usage := resp.UsageMetadata.toCore()
	usageTokens = obs.UsageFromCore(usage)
	return &core.TextResult{
		Text:         text,
		Model:        model,
		Provider:     "gemini",
		FinishReason: core.StopReason{Type: resp.Candidates[0].FinishReason},
		Usage:        usage,
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

func (c *Client) GenerateObject(ctx context.Context, req core.Request) (_ *core.ObjectResultRaw, err error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.gemini.GenerateObject",
		attribute.String("ai.provider", "gemini"),
		attribute.String("ai.operation", "generateContent"),
	)
	var usageTokens obs.UsageTokens
	defer func() {
		recorder.End(err, usageTokens)
	}()

	clone := req.Clone()
	clone.ProviderOptions = cloneAnyMap(clone.ProviderOptions)
	if clone.ProviderOptions == nil {
		clone.ProviderOptions = map[string]any{}
	}
	if _, ok := clone.ProviderOptions[providerOptionResponseMIME]; !ok {
		clone.ProviderOptions[providerOptionResponseMIME] = "application/json"
	}

	model := chooseModel(clone.Model, c.opts.model)
	clone.Model = model
	payload, err := buildRequest(clone, model)
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
	text := strings.TrimSpace(resp.JoinText())
	if text == "" {
		return nil, errors.New("gemini: empty structured response")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		return nil, fmt.Errorf("gemini: structured output is not valid json: %w", err)
	}
	usage := resp.UsageMetadata.toCore()
	usageTokens = obs.UsageFromCore(usage)
	return &core.ObjectResultRaw{
		JSON:     compact.Bytes(),
		Model:    model,
		Provider: "gemini",
		Usage:    usage,
	}, nil
}

func (c *Client) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	ctx, recorder := obs.StartRequest(ctx, "providers.gemini.StreamObject",
		attribute.String("ai.provider", "gemini"),
		attribute.String("ai.operation", "streamGenerateContent"),
	)
	clone := req.Clone()
	clone.ProviderOptions = cloneAnyMap(clone.ProviderOptions)
	if clone.ProviderOptions == nil {
		clone.ProviderOptions = map[string]any{}
	}
	if _, ok := clone.ProviderOptions[providerOptionResponseMIME]; !ok {
		clone.ProviderOptions[providerOptionResponseMIME] = "application/json"
	}

	model := chooseModel(clone.Model, c.opts.model)
	clone.Model = model
	payload, err := buildRequest(clone, model)
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
	request := &geminiRequest{
		Model:    model,
		Contents: contents,
		GenerationConfig: geminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
			TopP:            req.TopP,
		},
		SafetySettings: convertSafety(req.Safety),
	}
	if len(req.Tools) > 0 {
		tools, err := convertTools(req.Tools)
		if err != nil {
			return nil, err
		}
		request.Tools = tools
		request.ToolConfig = &geminiToolConfig{FunctionCallingConfig: &geminiFunctionCallingConfig{Mode: toGeminiToolMode(req.ToolChoice)}}
		if request.ToolConfig.FunctionCallingConfig.Mode == "ANY" {
			names := make([]string, 0, len(req.Tools))
			for _, handle := range req.Tools {
				if handle != nil {
					names = append(names, handle.Name())
				}
			}
			if len(names) > 0 {
				request.ToolConfig.FunctionCallingConfig.AllowedFunctionNames = names
			}
		}
	}
	if len(req.ProviderOptions) > 0 {
		if err := applyProviderOptions(request, req.ProviderOptions); err != nil {
			return nil, err
		}
	}
	return request, nil
}

func consumeStream(body io.ReadCloser, stream *core.Stream) {
    defer body.Close()
    defer stream.Close()

    scanner := bufio.NewScanner(body)
    scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
    seq := 0
    var buffer strings.Builder
    // Track the latest usage reported in streamed chunks
    var lastUsage core.Usage
    // Generate a per-stream nonce to ensure tool call IDs are unique across steps
    // even if providers reuse simple counters like "call_1".
    streamNonce := fmt.Sprintf("s%x", time.Now().UnixNano())
    pushedFinish := false

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
            // Include the last observed usage in the finish event so token
            // accounting is available to runners and UIs.
            stream.Push(core.StreamEvent{Type: core.EventFinish, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini", Usage: lastUsage})
            pushedFinish = true
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
                        emitStreamEvents(stream, item, &seq, streamNonce, &lastUsage)
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

        emitStreamEvents(stream, resp, &seq, streamNonce, &lastUsage)
        flushBuffer()
    }
    if err := scanner.Err(); err != nil {
        stream.Fail(err)
    }
    // Some providers (including Gemini) may close the stream without a [DONE] sentinel.
    // Emit a final finish event if none was pushed so downstream components can
    // finalize properly and propagate usage.
    if !pushedFinish {
        seq++
        stream.Push(core.StreamEvent{Type: core.EventFinish, Seq: seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini", Usage: lastUsage})
    }
}

func emitStreamEvents(stream *core.Stream, resp geminiStreamResponse, seq *int, streamNonce string, lastUsage *core.Usage) {
    if text := resp.JoinText(); text != "" {
        *seq++
        stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: text, Seq: *seq, Schema: "gai.events.v1", Timestamp: time.Now(), Provider: "gemini"})
    }
    // Capture usage if present in this chunk so we can publish it on finish.
    if (resp.UsageMetadata != geminiUsageMetadata{}) {
        if lastUsage != nil {
            *lastUsage = resp.UsageMetadata.toCore()
        }
    }
    for _, cand := range resp.Candidates {
        rawContent := marshalGeminiContent(cand.Content)
        for _, part := range cand.Content.Parts {
            if part.FunctionCall == nil {
                continue
            }
            callID := strings.TrimSpace(part.FunctionCall.ID)
            if callID == "" {
                callID = fmt.Sprintf("call_%d", *seq+1)
            }
            // Prefix IDs with a per-stream nonce to avoid collisions across steps.
            // This ensures tool call IDs remain unique for the UI and logs while
            // preserving pairing with tool results (runner uses the same ID).
            callID = fmt.Sprintf("%s:%s", streamNonce, callID)
            args := cloneMap(part.FunctionCall.Args)
            metadata := map[string]any{}
            if rawContent != "" {
                metadata[metadataKeyRawContent] = rawContent
            }
			if sig := strings.TrimSpace(part.ThoughtSignature); sig != "" {
				metadata[metadataKeyThoughtSignature] = sig
			}
			if part.Thought {
				metadata[metadataKeyThought] = true
			}
			toolCall := core.ToolCall{
				ID:       callID,
				Name:     part.FunctionCall.Name,
				Input:    args,
				Metadata: nil,
			}
			if len(metadata) > 0 {
				toolCall.Metadata = metadata
			}
			if toolCall.Input == nil {
				toolCall.Input = map[string]any{}
			}
			*seq++
			stream.Push(core.StreamEvent{
				Type:      core.EventToolCall,
				ToolCall:  toolCall,
				Seq:       *seq,
				Schema:    "gai.events.v1",
				Timestamp: time.Now(),
				Provider:  "gemini",
				Ext:       cloneAnyMap(metadata),
			})
		}
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

		if raw, ok := message.Metadata[metadataKeyRawContent]; ok {
			if content, ok := restoreGeminiContent(raw); ok {
				if content.Role == "" {
					content.Role = roleFromMessageRole(message.Role)
				}
				contents = append(contents, content)
				continue
			}
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
				if p.Text == "" {
					continue
				}
				parts = append(parts, geminiPart{Text: p.Text})
			case core.Image:
				inline, err := inlineDataFromBlob(p.Source)
				if err != nil {
					return nil, err
				}
				parts = append(parts, geminiPart{InlineData: inline})
			case core.ImageURL:
				inline, err := inlineDataFromURL(p.URL, p.MIME)
				if err != nil {
					return nil, err
				}
				parts = append(parts, geminiPart{InlineData: inline})
			case core.Video:
				inline, err := inlineDataFromVideo(p)
				if err != nil {
					return nil, err
				}
				parts = append(parts, geminiPart{InlineData: inline})
			case core.ToolCall:
				args := cloneMap(p.Input)
				fc := &geminiFunctionCall{Name: p.Name, Args: args}
				if p.ID != "" {
					fc.ID = p.ID
				}
				gemPart := geminiPart{FunctionCall: fc}
				if sig, ok := extractMetadataString(p.Metadata, metadataKeyThoughtSignature); ok {
					gemPart.ThoughtSignature = sig
				} else if sig, ok := extractMetadataString(message.Metadata, metadataKeyThoughtSignature); ok {
					gemPart.ThoughtSignature = sig
				}
				if thought, ok := p.Metadata[metadataKeyThought].(bool); ok && thought {
					gemPart.Thought = true
				} else if thought, ok := message.Metadata[metadataKeyThought].(bool); ok && thought {
					gemPart.Thought = true
				}
				parts = append(parts, gemPart)
			case core.ToolResult:
				response := makeFunctionResponsePayload(p)
				funcResp := &geminiFunctionResponse{Name: p.Name, Response: response}
				if p.ID != "" {
					funcResp.ID = p.ID
				}
				parts = append(parts, geminiPart{FunctionResponse: funcResp})
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

func roleFromMessageRole(role core.Role) string {
	switch role {
	case core.Assistant:
		return "model"
	case core.User:
		return "user"
	default:
		return "user"
	}
}

func inlineDataFromBlob(blob core.BlobRef) (*geminiInlineData, error) {
	return inlineDataFromBlobWithMIME(blob, "")
}

func inlineDataFromVideo(video core.Video) (*geminiInlineData, error) {
	mimeType := video.Source.MIME
	if mimeType == "" {
		if candidate := videoMIMEMapping[strings.TrimPrefix(strings.ToLower(video.Format), ".")]; candidate != "" {
			mimeType = candidate
		}
	}
	return inlineDataFromBlobWithMIME(video.Source, mimeType)
}

func inlineDataFromBlobWithMIME(blob core.BlobRef, override string) (*geminiInlineData, error) {
	if err := blob.Validate(); err != nil {
		return nil, fmt.Errorf("validate blob: %w", err)
	}
	if blob.Kind == core.BlobProvider {
		return nil, errors.New("gemini requires inline data or fileUri; provider-managed blobs are not supported")
	}
	data, err := blob.Read()
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	mimeType := override
	if mimeType == "" {
		mimeType = blob.MIME
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "" {
		return nil, errors.New("unable to determine MIME type for inline data")
	}
	return &geminiInlineData{MimeType: mimeType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func inlineDataFromURL(resourceURL, mimeHint string) (*geminiInlineData, error) {
	if strings.TrimSpace(resourceURL) == "" {
		return nil, errors.New("url is required for inline data")
	}
	resp, err := http.Get(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", resourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: status %s", resourceURL, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", resourceURL, err)
	}
	mimeType := strings.TrimSpace(mimeHint)
	if mimeType == "" {
		mimeType = strings.TrimSpace(resp.Header.Get("Content-Type"))
		if idx := strings.Index(mimeType, ";"); idx >= 0 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if mimeType == "" {
		return nil, fmt.Errorf("unable to determine MIME type for %s", resourceURL)
	}
	return &geminiInlineData{MimeType: mimeType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

var videoMIMEMapping = map[string]string{
	"mp4":  "video/mp4",
	"mov":  "video/quicktime",
	"m4v":  "video/x-m4v",
	"webm": "video/webm",
	"mkv":  "video/x-matroska",
	"ogg":  "video/ogg",
	"ogv":  "video/ogg",
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

func convertTools(handles []core.ToolHandle) ([]geminiTool, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	decls := make([]geminiFunctionDeclaration, 0, len(handles))
	for _, handle := range handles {
		if handle == nil {
			continue
		}
		schemaMap, err := schemaToMap(handle.InputSchema())
		if err != nil {
			return nil, err
		}
		decls = append(decls, geminiFunctionDeclaration{
			Name:        handle.Name(),
			Description: handle.Description(),
			Parameters:  schemaMap,
		})
	}
	if len(decls) == 0 {
		return nil, nil
	}
	return []geminiTool{{FunctionDeclarations: decls}}, nil
}

func applyProviderOptions(request *geminiRequest, opts map[string]any) error {
	if mime, ok := getString(opts, providerOptionResponseMIME); ok {
		request.GenerationConfig.ResponseMimeType = mime
	}
	budget, ok := getInt(opts, providerOptionThinkingBudget)
	if ok {
		if request.GenerationConfig.ThinkingConfig == nil {
			request.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{}
		}
		request.GenerationConfig.ThinkingConfig.ThinkingBudget = budget
	}
	if include, ok := getBool(opts, providerOptionIncludeThought); ok {
		if request.GenerationConfig.ThinkingConfig == nil {
			request.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{}
		}
		request.GenerationConfig.ThinkingConfig.IncludeThoughts = include
	}
	return nil
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

func toGeminiToolMode(choice core.ToolChoice) string {
	switch choice {
	case core.ToolChoiceNone:
		return "NONE"
	case core.ToolChoiceRequired:
		return "ANY"
	default:
		return "AUTO"
	}
}

func makeFunctionResponsePayload(result core.ToolResult) map[string]any {
	response := map[string]any{}
	switch v := result.Result.(type) {
	case nil:
		// no data
	case map[string]any:
		for key, value := range v {
			response[key] = value
		}
	case string:
		response["text"] = v
	default:
		response["output"] = v
	}
	if result.Error != "" {
		response["error"] = result.Error
	}
	return response
}

func marshalGeminiContent(content geminiContent) string {
	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(data)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func restoreGeminiContent(raw any) (geminiContent, bool) {
	switch v := raw.(type) {
	case string:
		var content geminiContent
		if err := json.Unmarshal([]byte(v), &content); err == nil {
			return content, true
		}
	case []byte:
		var content geminiContent
		if err := json.Unmarshal(v, &content); err == nil {
			return content, true
		}
	case map[string]any:
		if data, err := json.Marshal(v); err == nil {
			var content geminiContent
			if err := json.Unmarshal(data, &content); err == nil {
				return content, true
			}
		}
	case geminiContent:
		return v, true
	}
	return geminiContent{}, false
}

func extractMetadataString(meta map[string]any, key string) (string, bool) {
	if meta == nil {
		return "", false
	}
	val, ok := meta[key]
	if !ok {
		return "", false
	}
	switch v := val.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	}
	return "", false
}

func getInt(opts map[string]any, key string) (int, bool) {
	val, ok := opts[key]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
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
	case string:
		i, err := strconv.Atoi(v)
		if err == nil {
			return i, true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func getBool(opts map[string]any, key string) (bool, bool) {
	val, ok := opts[key]
	if !ok {
		return false, false
	}
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed, true
		}
	}
	return false, false
}

func getString(opts map[string]any, key string) (string, bool) {
	val, ok := opts[key]
	if !ok {
		return "", false
	}
	switch v := val.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case json.Number:
		trimmed := strings.TrimSpace(v.String())
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case fmt.Stringer:
		trimmed := strings.TrimSpace(v.String())
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	default:
		trimmed := strings.TrimSpace(fmt.Sprint(v))
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
}

func chooseModel(request, fallback string) string {
	if request != "" {
		return request
	}
	return fallback
}

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/runner"
)

type chatHandler struct {
	providers map[string]providerEntry
	firecrawl *firecrawlClient
}

type apiMessage struct {
	Role     string         `json:"role"`
	Parts    []apiPart      `json:"parts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type apiPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Data     string         `json:"data,omitempty"`
	Mime     string         `json:"mime,omitempty"`
	ID       string         `json:"id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type chatRequest struct {
	Provider        string         `json:"provider"`
	Model           string         `json:"model,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	Messages        []apiMessage   `json:"messages"`
	Temperature     float32        `json:"temperature,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	ToolChoice      string         `json:"tool_choice,omitempty"`
	Tools           []string       `json:"tools,omitempty"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

type chatResponse struct {
	ID           string          `json:"id"`
	Text         string          `json:"text"`
	JSON         any             `json:"json,omitempty"`
	Model        string          `json:"model"`
	Provider     string          `json:"provider"`
	Usage        core.Usage      `json:"usage"`
	FinishReason core.StopReason `json:"finish_reason"`
	Steps        []stepDTO       `json:"steps"`
	Warnings     []core.Warning  `json:"warnings,omitempty"`
}

type stepDTO struct {
	Number    int                `json:"number"`
	Text      string             `json:"text"`
	Model     string             `json:"model"`
	Duration  int64              `json:"duration_ms"`
	ToolCalls []toolExecutionDTO `json:"tool_calls"`
}

type toolExecutionDTO struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input,omitempty"`
	Result   any            `json:"result,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration int64          `json:"duration_ms,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Retries  int            `json:"retries,omitempty"`
}

type providerListResponse struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	DefaultModel string            `json:"default_model"`
	Models       []string          `json:"models"`
	Capabilities core.Capabilities `json:"capabilities"`
	Tools        []string          `json:"tools"`
}

func (h *chatHandler) handleProviders(w http.ResponseWriter, r *http.Request) {
	list := make([]providerListResponse, 0, len(h.providers))
	for id, entry := range h.providers {
		caps := entry.Client.Capabilities()
		tools := []string{}
		if h.firecrawl != nil && h.firecrawl.enabled() {
			tools = append(tools, "web_search", "url_extract")
		}
		list = append(list, providerListResponse{
			ID:           id,
			Label:        entry.Label,
			DefaultModel: entry.DefaultModel,
			Models:       entry.Models,
			Capabilities: caps,
			Tools:        tools,
		})
	}
	writeJSON(w, http.StatusOK, list)
	log.Printf("providers request served (%d providers)", len(list))
}

func (h *chatHandler) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages are required")
		return
	}

	ctx := r.Context()
	start := time.Now()
	reqID := uuid.NewString()

	requestCore, entry, err := h.prepareCoreRequest(req, reqID)
	if err != nil {
		if errors.Is(err, errUnknownProvider) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("chat request provider=%s mode=%s streaming=false", entry.Label, req.Mode)

	if strings.EqualFold(req.Mode, "json") {
		h.handleJSONMode(ctx, w, requestCore, entry, reqID, start)
		return
	}

	run := runner.New(entry.Client,
		runner.WithOnToolError(runner.ToolErrorAppendAndContinue),
		runner.WithToolTimeout(25*time.Second),
	)

	result, err := run.ExecuteRequest(ctx, requestCore)
	if err != nil {
		log.Printf("chat error: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := chatResponse{
		ID:           uuid.NewString(),
		Text:         strings.TrimSpace(result.Text),
		Model:        result.Model,
		Provider:     entry.Client.Capabilities().Provider,
		Usage:        result.Usage,
		FinishReason: result.FinishReason,
		Warnings:     result.Warnings,
	}
	resp.Steps = convertSteps(result.Steps)

	latency := time.Since(start)
	metadata := map[string]any{"mode": "text", "streaming": false}
	if len(result.Warnings) > 0 {
		metadata["warnings"] = result.Warnings
	}

	obs.LogCompletion(ctx, obs.Completion{
		Provider:     resp.Provider,
		Model:        result.Model,
		RequestID:    reqID,
		Input:        obs.MessagesFromCore(requestCore.Messages),
		Output:       obs.MessageFromCore(core.AssistantMessage(resp.Text)),
		Usage:        obs.UsageFromCore(result.Usage),
		LatencyMS:    latency.Milliseconds(),
		Metadata:     metadata,
		ToolCalls:    obs.ToolCallsFromSteps(result.Steps),
		CreatedAtUTC: time.Now().UTC().UnixMilli(),
	})

	writeJSON(w, http.StatusOK, resp)
	log.Printf("chat response sent (latency=%s)", latency)
}

func (h *chatHandler) handleJSONMode(ctx context.Context, w http.ResponseWriter, request core.Request, entry providerEntry, reqID string, start time.Time) {
	result, err := entry.Client.GenerateObject(ctx, request)
	if err != nil {
		log.Printf("chat error (json mode): %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var payload any
	if len(result.JSON) > 0 {
		if err := json.Unmarshal(result.JSON, &payload); err != nil {
			payload = string(result.JSON)
		}
	}

	resp := chatResponse{
		ID:           uuid.NewString(),
		JSON:         payload,
		Model:        result.Model,
		Provider:     entry.Client.Capabilities().Provider,
		Usage:        result.Usage,
		FinishReason: core.StopReason{Type: core.StopReasonProviderFinish},
	}

	latency := time.Since(start)
	obs.LogCompletion(ctx, obs.Completion{
		Provider:     resp.Provider,
		Model:        result.Model,
		RequestID:    reqID,
		Input:        obs.MessagesFromCore(request.Messages),
		Output:       obs.MessageFromCore(core.AssistantMessage(string(result.JSON))),
		Usage:        obs.UsageFromCore(result.Usage),
		LatencyMS:    latency.Milliseconds(),
		Metadata:     map[string]any{"mode": "json", "streaming": false},
		CreatedAtUTC: time.Now().UTC().UnixMilli(),
	})

	writeJSON(w, http.StatusOK, resp)
	log.Printf("chat response sent (json mode, latency=%s)", latency)
}

func (h *chatHandler) prepareCoreRequest(req chatRequest, reqID string) (core.Request, providerEntry, error) {
	entry, ok := h.providers[req.Provider]
	if !ok {
		return core.Request{}, providerEntry{}, errUnknownProvider
	}

	messages, err := convertAPIMessages(req.Messages)
	if err != nil {
		return core.Request{}, providerEntry{}, err
	}

	selected := make(map[string]struct{}, len(req.Tools))
	for _, name := range req.Tools {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			selected[trimmed] = struct{}{}
		}
	}

	toolHandles := make([]core.ToolHandle, 0, 4)
	if h.firecrawl != nil && h.firecrawl.enabled() {
		if len(selected) == 0 || hasTool(selected, "web_search") {
			if handle := h.firecrawl.searchTool(); handle != nil {
				toolHandles = append(toolHandles, handle)
			}
		}
		if len(selected) == 0 || hasTool(selected, "url_extract") {
			if handle := h.firecrawl.extractTool(); handle != nil {
				toolHandles = append(toolHandles, handle)
			}
		}
	}

	providerOptions := cloneAnyMap(req.ProviderOptions)
	if providerOptions == nil {
		providerOptions = map[string]any{}
	}

	metadata := map[string]any{"request_id": reqID}

	request := core.Request{
		Model:           chooseModel(req.Model, entry.DefaultModel),
		Messages:        messages,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxOutputTokens,
		Tools:           toolHandles,
		ToolChoice:      parseToolChoice(req.ToolChoice),
		ProviderOptions: providerOptions,
		Metadata:        metadata,
	}

	return request, entry, nil
}

func convertSteps(steps []core.Step) []stepDTO {
	if len(steps) == 0 {
		return nil
	}
	out := make([]stepDTO, 0, len(steps))
	for _, step := range steps {
		dto := stepDTO{
			Number:   step.Number,
			Text:     strings.TrimSpace(step.Text),
			Model:    step.Model,
			Duration: step.DurationMS,
		}
		if len(step.ToolCalls) > 0 {
			dto.ToolCalls = make([]toolExecutionDTO, 0, len(step.ToolCalls))
			for _, exec := range step.ToolCalls {
				dto.ToolCalls = append(dto.ToolCalls, toolExecutionDTO{
					ID:       exec.Call.ID,
					Name:     exec.Call.Name,
					Input:    obs.NormalizeMap(exec.Call.Input),
					Result:   normalizeAny(exec.Result),
					Error:    errorToString(exec.Error),
					Duration: exec.DurationMS,
					Metadata: obs.NormalizeMap(exec.Call.Metadata),
					Retries:  exec.Retries,
				})
			}
		}
		out = append(out, dto)
	}
	return out
}

func convertAPIMessages(msgs []apiMessage) ([]core.Message, error) {
	converted := make([]core.Message, 0, len(msgs))
	for _, msg := range msgs {
		role := core.Role(strings.ToLower(strings.TrimSpace(msg.Role)))
		switch role {
		case core.System, core.User, core.Assistant:
		default:
			return nil, fmt.Errorf("unsupported role %s", msg.Role)
		}
		parts := make([]core.Part, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch strings.ToLower(part.Type) {
			case "text":
				parts = append(parts, core.Text{Text: part.Text})
			case "image_base64":
				data, err := base64.StdEncoding.DecodeString(part.Data)
				if err != nil {
					return nil, fmt.Errorf("invalid image data: %w", err)
				}
				mime := part.Mime
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, core.Image{Source: core.BlobRef{Kind: core.BlobBytes, Bytes: data, MIME: mime, Size: int64(len(data))}})
			case "image_url":
				if strings.TrimSpace(part.Data) == "" {
					return nil, errors.New("image_url part requires data")
				}
				parts = append(parts, core.ImageURL{URL: part.Data, MIME: part.Mime})
			case "function_call":
				args := map[string]any{}
				if part.Text != "" {
					if err := json.Unmarshal([]byte(part.Text), &args); err != nil {
						return nil, fmt.Errorf("invalid function call args: %w", err)
					}
				}
				parts = append(parts, core.ToolCall{
					ID:       part.ID,
					Name:     part.Mime,
					Input:    args,
					Metadata: cloneMap(part.Metadata),
				})
			case "function_response":
				var response map[string]any
				if part.Text != "" {
					if err := json.Unmarshal([]byte(part.Text), &response); err != nil {
						return nil, fmt.Errorf("invalid function response: %w", err)
					}
				}
				if response == nil {
					response = map[string]any{}
				}
				parts = append(parts, core.ToolResult{ID: part.ID, Name: part.Mime, Result: response})
			default:
				return nil, fmt.Errorf("unsupported part type %s", part.Type)
			}
		}
		converted = append(converted, core.Message{Role: role, Parts: parts, Metadata: cloneMap(msg.Metadata)})
	}
	return converted, nil
}

func chooseModel(requested, fallback string) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	return fallback
}

func parseToolChoice(choice string) core.ToolChoice {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "none":
		return core.ToolChoiceNone
	case "required":
		return core.ToolChoiceRequired
	default:
		return core.ToolChoiceAuto
	}
}

func hasTool(set map[string]struct{}, name string) bool {
	_, ok := set[name]
	return ok
}

func normalizeAny(v any) any {
	if v == nil {
		return nil
	}
	switch typed := v.(type) {
	case string, float64, float32, int, int64, uint64, bool, map[string]any, []any:
		return typed
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return string(data)
		}
		return generic
	}
}

func errorToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
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

var errUnknownProvider = errors.New("unknown provider")

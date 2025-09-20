package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/openai"
	"github.com/shillcollin/gai/stream"
	"github.com/shillcollin/gai/tools"
)

func main() {
	if err := loadDotEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	app, err := newApp()
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/providers", app.handleProviders)
	mux.HandleFunc("/api/chat/stream", app.handleStream)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	addr := ":8080"
	if fromEnv := os.Getenv("PORT"); fromEnv != "" {
		addr = ":" + fromEnv
	}

	log.Printf("ent backend listening on %s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loadDotEnv() error {
	paths := []string{".env", filepath.Join("ent", ".env"), filepath.Join("ent", "backend", ".env")}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
		return nil
	}
	return os.ErrNotExist
}

type weatherInput struct {
	Location string `json:"location"`
	Units    string `json:"units,omitempty"`
}

type weatherOutput struct {
	Summary string  `json:"summary"`
	Temp    float64 `json:"temp"`
	Units   string  `json:"units"`
}

type timeInput struct {
	Format string `json:"format,omitempty"`
}

type timeOutput struct {
	Current string `json:"current"`
}

type providerEntry struct {
	Name    string            `json:"name"`
	Label   string            `json:"label"`
	Models  []string          `json:"models"`
	Default string            `json:"default_model"`
	Kind    string            `json:"kind"`
	Client  core.Provider     `json:"-"`
	Caps    core.Capabilities `json:"-"`
}

type app struct {
	providers   map[string]*providerEntry
	toolHandles []core.ToolHandle
}

func newApp() (*app, error) {
	providers := map[string]*providerEntry{}

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		client := openai.New(
			openai.WithAPIKey(key),
			openai.WithModel("gpt-4o-mini"),
		)
		providers["openai"] = &providerEntry{
			Name:    "openai",
			Label:   "OpenAI",
			Models:  []string{"gpt-4o-mini", "gpt-4o", "gpt-4o-mini-2024-07-18"},
			Default: "gpt-4o-mini",
			Kind:    "chat_completions",
			Client:  client,
		}
	}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		client := anthropic.New(
			anthropic.WithAPIKey(key),
			anthropic.WithModel("claude-3-haiku-20240307"),
		)
		providers["anthropic"] = &providerEntry{
			Name:    "anthropic",
			Label:   "Anthropic",
			Models:  []string{"claude-3-haiku-20240307", "claude-3-5-sonnet-20240620"},
			Default: "claude-3-haiku-20240307",
			Kind:    "messages",
			Client:  client,
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers configured")
	}

	for _, entry := range providers {
		entry.Caps = entry.Client.Capabilities()
	}

	weatherTool := tools.New[weatherInput, weatherOutput]("get_weather", "Returns demo weather information", func(ctx context.Context, in weatherInput, meta core.ToolMeta) (weatherOutput, error) {
		units := in.Units
		if units == "" {
			units = "fahrenheit"
		}
		return weatherOutput{
			Summary: fmt.Sprintf("Weather in %s is pleasant.", fallback(in.Location, "San Francisco")),
			Temp:    72.0,
			Units:   units,
		}, nil
	})

	timeTool := tools.New[timeInput, timeOutput]("get_time", "Returns the current server time", func(ctx context.Context, in timeInput, meta core.ToolMeta) (timeOutput, error) {
		layout := time.RFC3339
		if in.Format != "" {
			layout = in.Format
		}
		return timeOutput{Current: time.Now().Format(layout)}, nil
	})

	return &app{
		providers:   providers,
		toolHandles: []core.ToolHandle{tools.NewCoreAdapter(weatherTool), tools.NewCoreAdapter(timeTool)},
	}, nil
}

func (a *app) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list := make([]providerEntry, 0, len(a.providers))
	for _, p := range a.providers {
		list = append(list, providerEntry{
			Name:    p.Name,
			Label:   p.Label,
			Models:  p.Models,
			Default: p.Default,
			Kind:    p.Kind,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (a *app) handleStream(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	log.Printf("stream_request method=%s path=%s", r.Method, r.URL.Path)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		log.Printf("stream_request invalid_method method=%s", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		log.Printf("stream_request invalid_payload error=%v", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	providerEntry, ok := a.providers[payload.Provider]
	if !ok {
		log.Printf("stream_request unknown_provider provider=%s", payload.Provider)
		http.Error(w, "provider not configured", http.StatusBadRequest)
		return
	}

	model := payload.Model
	if model == "" {
		model = providerEntry.Default
	}

	messages, err := toCoreMessages(payload.Messages)
	if err != nil {
		log.Printf("stream_request invalid_messages error=%v", err)
		http.Error(w, fmt.Sprintf("invalid messages: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	log.Printf("stream_request provider=%s model=%s messages=%d", providerEntry.Name, model, len(messages))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sse := stream.NewSSEWriter(w)
	defer func() {
		if err := sse.Close(); err != nil {
			log.Printf("stream_request close_error provider=%s model=%s error=%v", providerEntry.Name, model, err)
		}
		log.Printf("stream_request finished provider=%s model=%s duration=%s", providerEntry.Name, model, time.Since(start))
	}()

	seq := 0
	send := func(ev core.StreamEvent) error {
		seq++
		if ev.Schema == "" {
			ev.Schema = "gai.events.v1"
		}
		ev.Seq = seq
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now().UTC()
		}
		if err := sse.Write(ev); err != nil {
			return err
		}
		return sse.Flush()
	}

	if err := send(core.StreamEvent{
		Type:         core.EventStart,
		Capabilities: capabilitiesFrom(providerEntry.Caps),
		Policies:     map[string]any{"reasoning_visibility": payload.ReasoningMode},
	}); err != nil {
		return
	}

	transcripts := append([]core.Message(nil), messages...)
	maxSteps := payload.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}

	toolHandles := []core.ToolHandle{}
	if providerEntry.Caps.ParallelToolCalls {
		toolHandles = a.toolHandles
	}

	for step := 0; step < maxSteps; step++ {
		req := core.Request{
			Model:           model,
			Messages:        transcripts,
			Temperature:     payload.Temperature,
			MaxTokens:       payload.MaxTokens,
			TopP:            payload.TopP,
			Tools:           toolHandles,
			ToolChoice:      core.ToolChoiceAuto,
			ProviderOptions: payload.ProviderOptions,
			Stream:          true,
		}

		log.Printf("stream_step_start provider=%s model=%s step=%d", providerEntry.Name, model, step+1)
		streamResp, err := providerEntry.Client.StreamText(ctx, req)
		if err != nil {
			log.Printf("stream_step_start_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, step+1, err)
			_ = send(core.StreamEvent{
				Type:  core.EventError,
				Error: err,
				Ext:   map[string]any{"provider": providerEntry.Name, "model": model, "stage": "start", "message": err.Error()},
			})
			return
		}

		toolCalls := make([]core.ToolCall, 0)
		for event := range streamResp.Events() {
			select {
			case <-ctx.Done():
				streamResp.Close()
				return
			default:
			}
			switch event.Type {
			case core.EventToolCall:
				toolCalls = append(toolCalls, event.ToolCall)
				log.Printf("stream_tool_call provider=%s model=%s step=%d id=%s name=%s", providerEntry.Name, model, step+1, event.ToolCall.ID, event.ToolCall.Name)
			case core.EventError:
				log.Printf("stream_event_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, step+1, event.Error)
			case core.EventFinish:
				log.Printf("stream_finish provider=%s model=%s step=%d reason=%v", providerEntry.Name, model, step+1, event.FinishReason)
			case core.EventReasoningDelta:
				log.Printf("stream_reasoning_delta provider=%s model=%s step=%d", providerEntry.Name, model, step+1)
			}
			if err := send(event); err != nil {
				log.Printf("stream_send_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, step+1, err)
				streamResp.Close()
				return
			}
		}
		if err := streamResp.Err(); err != nil {
			log.Printf("stream_err provider=%s model=%s step=%d error=%v", providerEntry.Name, model, step+1, err)
			_ = send(core.StreamEvent{
				Type:  core.EventError,
				Error: err,
				Ext:   map[string]any{"provider": providerEntry.Name, "model": model, "stage": "stream", "message": err.Error()},
			})
			return
		}

		if len(toolCalls) == 0 {
			break
		}

		for _, call := range toolCalls {
			transcripts = append(transcripts, core.Message{Role: core.Assistant, Parts: []core.Part{call}})
			resultEvent := a.executeTool(ctx, call, step+1)
			if err := send(resultEvent); err != nil {
				return
			}
			if resultEvent.Error != nil {
				transcripts = append(transcripts, core.Message{Role: core.Assistant, Parts: []core.Part{core.ToolResult{ID: call.ID, Name: call.Name, Error: resultEvent.Error.Error()}}})
			} else {
				transcripts = append(transcripts, core.Message{Role: core.Assistant, Parts: []core.Part{core.ToolResult{ID: call.ID, Name: call.Name, Result: resultEvent.ToolResult.Result}}})
			}
		}
	}

	if err := send(core.StreamEvent{Type: core.EventFinish, FinishReason: &core.StopReason{Type: "complete"}}); err != nil {
		log.Printf("stream_complete_send_error provider=%s model=%s error=%v", providerEntry.Name, model, err)
	}
}

func (a *app) executeTool(ctx context.Context, call core.ToolCall, stepID int) core.StreamEvent {
	handle := a.findTool(call.Name)
	if handle == nil {
		log.Printf("tool_not_found name=%s", call.Name)
		return core.StreamEvent{
			Type:  core.EventError,
			Error: fmt.Errorf("tool %s not found", call.Name),
			Ext:   map[string]any{"tool": call.Name},
		}
	}
	result, err := handle.Execute(ctx, call.Input, core.ToolMeta{CallID: call.ID, StepID: stepID})
	if err != nil {
		log.Printf("tool_error name=%s error=%v", call.Name, err)
		return core.StreamEvent{Type: core.EventToolResult, ToolResult: core.ToolResult{ID: call.ID, Name: call.Name, Error: err.Error()}, StepID: stepID}
	}
	log.Printf("tool_success name=%s", call.Name)
	return core.StreamEvent{Type: core.EventToolResult, ToolResult: core.ToolResult{ID: call.ID, Name: call.Name, Result: result}, StepID: stepID}
}

func (a *app) findTool(name string) core.ToolHandle {
	for _, tool := range a.toolHandles {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

func toCoreMessages(messages []uiMessage) ([]core.Message, error) {
	out := make([]core.Message, 0, len(messages))
	for _, msg := range messages {
		role := core.Role(msg.Role)
		if role != core.User && role != core.Assistant && role != core.System {
			return nil, fmt.Errorf("unsupported role %s", msg.Role)
		}
		parts := make([]core.Part, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch part.Type {
			case "text":
				parts = append(parts, core.Text{Text: part.Text})
			default:
				return nil, fmt.Errorf("unsupported part type %s", part.Type)
			}
		}
		out = append(out, core.Message{Role: role, Parts: parts})
	}
	return out, nil
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type chatRequest struct {
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	Messages        []uiMessage    `json:"messages"`
	Temperature     float32        `json:"temperature"`
	TopP            float32        `json:"top_p"`
	MaxTokens       int            `json:"max_tokens"`
	ProviderOptions map[string]any `json:"provider_options"`
	ReasoningMode   string         `json:"reasoning_mode"`
	MaxSteps        int            `json:"max_steps"`
}

type uiMessage struct {
	Role  string   `json:"role"`
	Parts []uiPart `json:"parts"`
}

type uiPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func capabilitiesFrom(c core.Capabilities) []string {
	caps := []string{"text.delta"}
	if c.Reasoning {
		caps = append(caps, "reasoning.delta", "reasoning.summary")
	}
	if c.ParallelToolCalls {
		caps = append(caps, "tool.call", "tool.result")
	}
	if len(caps) == 0 {
		caps = append(caps, "text.delta")
	}
	return caps
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

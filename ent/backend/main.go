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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shillcollin/gai/core"
	enttools "github.com/shillcollin/gai/ent/backend/tools"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/openai"
	openairesponses "github.com/shillcollin/gai/providers/openai-responses"
	"github.com/shillcollin/gai/stream"
	"github.com/shillcollin/gai/tools"
)

func main() {
	if err := loadDotEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	shutdown, err := initObservability(context.Background())
	if err != nil {
		log.Printf("observability init error: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := shutdown(ctx); err != nil {
				log.Printf("observability shutdown error: %v", err)
			}
		}()
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
			Models:  []string{"gpt-4o-mini", "gpt-4o", "gpt-4o-mini-2024-07-18", "gpt-5-nano"},
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

	// Add OpenAI Responses API provider
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		client := openairesponses.New(
			openairesponses.WithAPIKey(key),
			openairesponses.WithModel("gpt-4o-mini"),
			openairesponses.WithDefaultInstructions("You are a helpful assistant."),
		)
		providers["openai-responses"] = &providerEntry{
			Name:    "openai-responses",
			Label:   "OpenAI Responses API",
			Models:  []string{"gpt-4o-mini", "gpt-4o", "gpt-4.1", "o3", "o4-mini", "gpt-5-nano"},
			Default: "gpt-4o-mini",
			Kind:    "responses",
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

	webSearchTool := enttools.NewWebSearchTool()
	urlExtractTool := enttools.NewURLExtractTool()

	return &app{
		providers: providers,
		toolHandles: []core.ToolHandle{
			tools.NewCoreAdapter(weatherTool),
			tools.NewCoreAdapter(timeTool),
			webSearchTool,
			urlExtractTool,
		},
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
	requestID := uuid.NewString()
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
		ev.RequestID = requestID
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

	maxSteps := payload.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}

	state := &core.RunnerState{
		Messages: append([]core.Message(nil), messages...),
		Steps:    []core.Step{},
		Usage:    core.Usage{},
	}
	completionLogged := false
	paramWarnings := make([]core.Warning, 0)

	logCompletion := func(err error) {
		if completionLogged {
			return
		}
		completion := obs.Completion{
			Provider:  providerEntry.Name,
			Model:     model,
			RequestID: requestID,
			Input:     obs.MessagesFromCore(messages),
			Output:    obs.Message{Role: string(core.Assistant), Text: state.LastText()},
			Usage:     obs.UsageFromCore(state.Usage),
			LatencyMS: time.Since(start).Milliseconds(),
			Metadata: map[string]any{
				"reasoning_mode": payload.ReasoningMode,
				"max_steps":      maxSteps,
				"steps_taken":    len(state.Steps),
			},
			CreatedAtUTC: time.Now().UTC().UnixMilli(),
		}
		if err != nil {
			completion.Error = err.Error()
		}
		if len(paramWarnings) > 0 {
			completion.Metadata["param_warnings"] = paramWarnings
		}
		obs.LogCompletion(ctx, completion)
		completionLogged = true
	}

	stopCond := core.CombineConditions(core.MaxSteps(maxSteps), core.NoMoreTools())
	toolHandles := []core.ToolHandle{}
	if providerEntry.Caps.ParallelToolCalls {
		toolHandles = a.toolHandles
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if stop, reason := stopCond(state); stop {
			if reason.Type == "" {
				reason.Type = core.StopReasonComplete
			}
			state.StopReason = reason
			if err := send(core.StreamEvent{Type: core.EventFinish, FinishReason: &reason}); err != nil {
				log.Printf("stream_finish_send_error provider=%s model=%s error=%v", providerEntry.Name, model, err)
			}
			logCompletion(nil)
			return
		}

		req := core.Request{
			Model:           model,
			Messages:        append([]core.Message(nil), state.Messages...),
			Temperature:     payload.Temperature,
			MaxTokens:       payload.MaxTokens,
			TopP:            payload.TopP,
			Tools:           toolHandles,
			ToolChoice:      core.ToolChoiceAuto,
			ProviderOptions: payload.ProviderOptions,
			Stream:          true,
		}

		stepIndex := len(state.Steps) + 1
		stepStart := time.Now()
		log.Printf("stream_step_start provider=%s model=%s step=%d", providerEntry.Name, model, stepIndex)
		streamResp, err := providerEntry.Client.StreamText(ctx, req)
		if err != nil {
			log.Printf("stream_step_start_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, stepIndex, err)
			_ = send(core.StreamEvent{
				Type:  core.EventError,
				Error: err,
				Ext:   map[string]any{"provider": providerEntry.Name, "model": model, "stage": "start", "message": err.Error()},
			})
			return
		}
		if warns := streamResp.Warnings(); len(warns) > 0 {
			for _, warn := range warns {
				log.Printf("stream_param_warning provider=%s model=%s field=%s code=%s msg=%s", providerEntry.Name, model, warn.Field, warn.Code, warn.Message)
			}
			paramWarnings = append(paramWarnings, warns...)
		}

		builder := &strings.Builder{}
		toolCalls := make([]core.ToolCall, 0)
		usage := core.Usage{}
		var finishReason *core.StopReason

		for event := range streamResp.Events() {
			if ctx.Err() != nil {
				streamResp.Close()
				return
			}
			switch event.Type {
			case core.EventTextDelta:
				builder.WriteString(event.TextDelta)
			case core.EventToolCall:
				toolCalls = append(toolCalls, event.ToolCall)
				log.Printf("stream_tool_call provider=%s model=%s step=%d id=%s name=%s", providerEntry.Name, model, stepIndex, event.ToolCall.ID, event.ToolCall.Name)
			case core.EventFinish:
				usage = event.Usage
				finishReason = event.FinishReason
			case core.EventError:
				log.Printf("stream_event_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, stepIndex, event.Error)
			}
			if err := send(event); err != nil {
				log.Printf("stream_send_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, stepIndex, err)
				streamResp.Close()
				return
			}
		}
		if err := streamResp.Err(); err != nil {
			log.Printf("stream_err provider=%s model=%s step=%d error=%v", providerEntry.Name, model, stepIndex, err)
			_ = send(core.StreamEvent{
				Type:  core.EventError,
				Error: err,
				Ext:   map[string]any{"provider": providerEntry.Name, "model": model, "stage": "stream", "message": err.Error()},
			})
			logCompletion(err)
			return
		}
		_ = streamResp.Close()

		assistantText := builder.String()
		state.Messages = append(state.Messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.Text{Text: assistantText}}})

		finishedAt := time.Now()
		step := core.Step{
			Number:      stepIndex,
			Text:        assistantText,
			Usage:       usage,
			DurationMS:  finishedAt.Sub(stepStart).Milliseconds(),
			StartedAt:   stepStart.UnixMilli(),
			CompletedAt: finishedAt.UnixMilli(),
		}

		if finishReason != nil && finishReason.Type != "" {
			state.StopReason = *finishReason
		}

		if len(toolCalls) == 0 {
			state.Steps = append(state.Steps, step)
			state.LastStep = &state.Steps[len(state.Steps)-1]
			state.Usage = aggregateUsage(state.Usage, usage)
			if state.StopReason.Type == "" {
				state.StopReason = core.StopReason{Type: core.StopReasonNoMoreTools}
			}
			continue
		}

		executions := make([]core.ToolExecution, 0, len(toolCalls))
		for _, call := range toolCalls {
			state.Messages = append(state.Messages, core.Message{Role: core.Assistant, Parts: []core.Part{call}})
			event, exec := a.executeTool(ctx, call, stepIndex)
			if err := send(event); err != nil {
				log.Printf("stream_tool_result_send_error provider=%s model=%s step=%d error=%v", providerEntry.Name, model, stepIndex, err)
				logCompletion(err)
				return
			}
			executions = append(executions, exec)
			if exec.Error != nil {
				state.Messages = append(state.Messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.ToolResult{ID: call.ID, Name: call.Name, Error: exec.Error.Error()}}})
			} else {
				state.Messages = append(state.Messages, core.Message{Role: core.User, Parts: []core.Part{core.ToolResult{ID: call.ID, Name: call.Name, Result: exec.Result}}})
			}
		}
		step.ToolCalls = executions
		state.Steps = append(state.Steps, step)
		state.LastStep = &state.Steps[len(state.Steps)-1]
		state.Usage = aggregateUsage(state.Usage, usage)
	}
}

func (a *app) executeTool(ctx context.Context, call core.ToolCall, stepID int) (core.StreamEvent, core.ToolExecution) {
	handle := a.findTool(call.Name)
	if handle == nil {
		err := fmt.Errorf("tool %s not found", call.Name)
		log.Printf("tool_not_found name=%s", call.Name)
		event := core.StreamEvent{Type: core.EventError, Error: err, Ext: map[string]any{"tool": call.Name}}
		return event, core.ToolExecution{Call: call, Error: err}
	}
	start := time.Now()
	result, err := handle.Execute(ctx, call.Input, core.ToolMeta{CallID: call.ID, StepID: stepID})
	duration := time.Since(start).Milliseconds()
	if err != nil {
		log.Printf("tool_error name=%s error=%v", call.Name, err)
		event := core.StreamEvent{Type: core.EventToolResult, ToolResult: core.ToolResult{ID: call.ID, Name: call.Name, Error: err.Error()}, StepID: stepID}
		return event, core.ToolExecution{Call: call, Error: err, DurationMS: duration}
	}
	log.Printf("tool_success name=%s", call.Name)
	event := core.StreamEvent{Type: core.EventToolResult, ToolResult: core.ToolResult{ID: call.ID, Name: call.Name, Result: result}, StepID: stepID}
	return event, core.ToolExecution{Call: call, Result: result, DurationMS: duration}
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

func aggregateUsage(total, step core.Usage) core.Usage {
	total.InputTokens += step.InputTokens
	total.OutputTokens += step.OutputTokens
	total.TotalTokens += step.TotalTokens
	total.ReasoningTokens += step.ReasoningTokens
	total.CachedInputTokens += step.CachedInputTokens
	total.AudioTokens += step.AudioTokens
	total.CostUSD += step.CostUSD
	return total
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func initObservability(ctx context.Context) (func(context.Context) error, error) {
	opts := obs.DefaultOptions()
	opts.ServiceName = envOr("OTEL_SERVICE_NAME", "gai-ent-backend")
	opts.Environment = envOr("GAI_ENV", "development")
	opts.Version = envOr("GAI_VERSION", "dev")

	switch strings.ToLower(envOr("GAI_OBS_EXPORTER", "otlp")) {
	case "stdout":
		opts.Exporter = obs.ExporterStdout
	case "none":
		opts.Exporter = obs.ExporterNone
	default:
		opts.Exporter = obs.ExporterOTLP
	}

	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		opts.Endpoint = endpoint
	}
	if insecure := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"); insecure != "" {
		val, _ := strconv.ParseBool(insecure)
		opts.Insecure = val
	}
	if ratio := os.Getenv("GAI_OBS_SAMPLE_RATIO"); ratio != "" {
		if parsed, err := strconv.ParseFloat(ratio, 64); err == nil {
			opts.SampleRatio = parsed
		}
	}

	if key := os.Getenv("BRAINTRUST_API_KEY"); key != "" {
		opts.Braintrust.Enabled = true
		opts.Braintrust.APIKey = key
		opts.Braintrust.Project = os.Getenv("BRAINTRUST_PROJECT_NAME")
		opts.Braintrust.ProjectID = os.Getenv("BRAINTRUST_PROJECT_ID")
		opts.Braintrust.Dataset = envOr("BRAINTRUST_DATASET", "gai-ent")
		opts.Braintrust.BaseURL = envOr("BRAINTRUST_BASE_URL", "")
	}

	if enabled, _ := strconv.ParseBool(os.Getenv("ARIZE_ENABLED")); enabled {
		opts.Arize.Enabled = true
	}

	return obs.Init(ctx, opts)
}

func envOr(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

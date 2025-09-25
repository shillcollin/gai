package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/providers/anthropic"
	"github.com/shillcollin/gai/providers/gemini"
	"github.com/shillcollin/gai/providers/openai"
	openairesponses "github.com/shillcollin/gai/providers/openai-responses"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/tools"
)

type providerEntry struct {
	Name    string
	Label   string
	Models  []string
	Default string
	Client  core.Provider
}

type providerOption struct {
	entry *providerEntry
	model string
}

type toolTranscript struct {
	StepID int
	Call   core.ToolCall
	Result core.ToolResult
}

type conversation struct {
	reader         *bufio.Reader
	providers      []*providerEntry
	current        *providerEntry
	currentModel   string
	runner         *runner.Runner
	toolHandle     core.ToolHandle
	messages       []core.Message
	conversationID string
	turn           int
	systemPrompt   string
	counterMu      sync.Mutex
	counter        int
}

func main() {
	if err := loadDotEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "failed to load .env: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "observability init warning: %v\n", err)
	}
	defer func() {
		if shutdownObs != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownObs(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "observability shutdown error: %v\n", err)
			}
		}
	}()

	providers, err := buildProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if len(providers) == 0 {
		fmt.Fprintln(os.Stderr, "no providers configured; ensure API keys are present in environment")
		os.Exit(1)
	}

	systemPrompt := "You are being evaluated for tool usage. A tool named `increment_counter` is available. Call it repeatedly to increment and observe the counter value. Keep calling it until it reaches 5, then acknowledge success succinctly."

	conv := &conversation{
		reader:         bufio.NewReader(os.Stdin),
		providers:      providers,
		current:        providers[0],
		currentModel:   providers[0].Default,
		runner:         newRunner(providers[0].Client),
		conversationID: uuid.NewString(),
		systemPrompt:   systemPrompt,
	}
	conv.toolHandle = newCounterTool(conv)
	conv.messages = []core.Message{core.SystemMessage(systemPrompt)}

	fmt.Println("gai CLI — type 'model' to switch provider/model, Ctrl+C to exit.")
	fmt.Println("Tool test: the assistant should call `increment_counter` until the counter reaches 5.")
	fmt.Printf("Current provider: %s (%s)\n\n", conv.current.Label, conv.currentModel)

	for {
		fmt.Print("You: ")
		text, err := conv.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println()
				return
			}
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if strings.EqualFold(text, "model") || text == "/" {
			if err := conv.pickModel(); err != nil {
				fmt.Fprintf(os.Stderr, "model selection error: %v\n", err)
			}
			continue
		}

		if err := conv.processTurn(ctx, text); err != nil {
			fmt.Fprintf(os.Stderr, "turn error: %v\n", err)
		}
	}
}

func (c *conversation) readLine() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (c *conversation) processTurn(ctx context.Context, userInput string) error {
	userMsg := core.UserMessage(core.TextPart(userInput))
	c.messages = append(c.messages, userMsg)

	req := core.Request{
		Model:           c.currentModel,
		Messages:        append([]core.Message(nil), c.messages...),
		Tools:           []core.ToolHandle{c.toolHandle},
		ToolChoice:      core.ToolChoiceAuto,
		StopWhen:        core.NoMoreTools(),
		Metadata:        map[string]any{"conversation_id": c.conversationID, "turn": strconv.Itoa(c.turn + 1)},
		ProviderOptions: map[string]any{},
	}

	turnID := uuid.NewString()
	start := time.Now()
	stream, err := c.runner.StreamRequest(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	textByStep := make(map[int]*strings.Builder)
	var lastStepID int
	var reasoningBuilder strings.Builder
	var toolRecords []toolTranscript
	toolByID := make(map[string]int)

	for event := range stream.Events() {
		if event.StepID > lastStepID {
			lastStepID = event.StepID
		}
		switch event.Type {
		case core.EventReasoningDelta, core.EventReasoningSummary:
			appendReasoning(&reasoningBuilder, event.ReasoningDelta, event.ReasoningSummary)
		case core.EventTextDelta:
			b := textByStep[event.StepID]
			if b == nil {
				b = &strings.Builder{}
				textByStep[event.StepID] = b
			}
			b.WriteString(event.TextDelta)
		case core.EventToolCall:
			idx := len(toolRecords)
			toolRecords = append(toolRecords, toolTranscript{StepID: event.StepID, Call: event.ToolCall})
			toolByID[event.ToolCall.ID] = idx
			printToolCall(event.ToolCall)
		case core.EventToolResult:
			result := event.ToolResult
			if idx, ok := toolByID[result.ID]; ok {
				toolRecords[idx].Result = result
			} else {
				toolRecords = append(toolRecords, toolTranscript{StepID: event.StepID, Result: result})
			}
			printToolResult(result)
		}
	}

	if err := stream.Err(); err != nil && !errors.Is(err, core.ErrStreamClosed) {
		return err
	}

	finalText := strings.TrimSpace(builderTextForStep(textByStep, lastStepID))
	if finalText == "" {
		finalText = "(no assistant response)"
	}

	for _, rec := range toolRecords {
		if rec.Call.Name != "" {
			c.messages = append(c.messages, core.Message{Role: core.Assistant, Parts: []core.Part{rec.Call}})
		}
		if rec.Result.Name != "" || rec.Result.Result != nil || rec.Result.Error != "" {
			c.messages = append(c.messages, core.Message{Role: core.User, Parts: []core.Part{rec.Result}})
		}
	}

	if strings.TrimSpace(finalText) != "" && finalText != "(no assistant response)" {
		assistantMsg := core.AssistantMessage(finalText)
		c.messages = append(c.messages, assistantMsg)
	}

	elapsed := time.Since(start)
	meta := stream.Meta()

	fmt.Println()
	if reasoningBuilder.Len() > 0 {
		fmt.Println("[Reasoning]")
		fmt.Println(strings.TrimSpace(reasoningBuilder.String()))
		fmt.Println()
	}
	if len(toolRecords) > 0 {
		fmt.Println("[Tools]")
		for _, rec := range toolRecords {
			if rec.Call.Name != "" {
				inputJSON := mustJSON(rec.Call.Input)
				fmt.Printf("- %s input: %s\n", rec.Call.Name, inputJSON)
			}
			if rec.Result.Name != "" {
				outputJSON := mustJSON(rec.Result.Result)
				if rec.Result.Error != "" {
					fmt.Printf("  error: %s\n", rec.Result.Error)
				} else {
					fmt.Printf("  output: %s\n", outputJSON)
				}
			}
		}
		fmt.Println()
	}

	toolCallRecords := buildToolCallRecords(toolRecords)

	fmt.Printf("Assistant (%s / %s):\n%s\n\n", c.current.Label, c.currentModel, finalText)
	fmt.Printf("Usage: input=%d output=%d total=%d (%.2fs)\n\n", meta.Usage.InputTokens, meta.Usage.OutputTokens, meta.Usage.TotalTokens, elapsed.Seconds())

	obs.LogCompletion(ctx, obs.Completion{
		Provider:     c.current.Name,
		Model:        c.currentModel,
		RequestID:    turnID,
		Input:        obs.MessagesFromCore(c.messages),
		Output:       obs.MessageFromCore(core.AssistantMessage(finalText)),
		Usage:        obs.UsageFromCore(meta.Usage),
		LatencyMS:    elapsed.Milliseconds(),
		Metadata:     map[string]any{"conversation_id": c.conversationID, "turn": strconv.Itoa(c.turn + 1), "tool_call_count": len(toolCallRecords)},
		ToolCalls:    toolCallRecords,
		CreatedAtUTC: time.Now().UTC().UnixMilli(),
	})

	c.turn++
	fmt.Printf("Current provider: %s (%s)\n\n", c.current.Label, c.currentModel)
	return nil
}

func appendReasoning(builder *strings.Builder, delta, summary string) {
	text := delta
	if text == "" {
		text = summary
	}
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString(" ")
	}
	builder.WriteString(strings.TrimSpace(text))
}

func builderTextForStep(m map[int]*strings.Builder, step int) string {
	if b := m[step]; b != nil {
		return b.String()
	}
	var maxStep int
	for s := range m {
		if s > maxStep {
			maxStep = s
		}
	}
	if b := m[maxStep]; b != nil {
		return b.String()
	}
	return ""
}

func printToolCall(call core.ToolCall) {
	inputJSON := mustJSON(call.Input)
	fmt.Printf("[Tool call] %s -> %s\n", call.Name, inputJSON)
}

func printToolResult(result core.ToolResult) {
	if result.Error != "" {
		fmt.Printf("[Tool error] %s: %s\n", result.Name, result.Error)
		return
	}
	outputJSON := mustJSON(result.Result)
	fmt.Printf("[Tool result] %s -> %s\n", result.Name, outputJSON)
}

func buildToolCallRecords(records []toolTranscript) []obs.ToolCallRecord {
	if len(records) == 0 {
		return nil
	}
	byKey := make(map[string]*obs.ToolCallRecord)
	order := make([]string, 0)
	fallback := 0
	ensure := func(id string, step int, name string) *obs.ToolCallRecord {
		key := id
		if key == "" {
			key = fmt.Sprintf("step%d_%d_%s", step, fallback, name)
			fallback++
		}
		rec, ok := byKey[key]
		if !ok {
			rec = &obs.ToolCallRecord{Step: step, ID: id, Name: name}
			byKey[key] = rec
			order = append(order, key)
		} else {
			if rec.Step == 0 && step != 0 {
				rec.Step = step
			}
			if rec.Name == "" {
				rec.Name = name
			}
		}
		return rec
	}

	for _, entry := range records {
		if entry.Call.Name != "" {
			rec := ensure(entry.Call.ID, entry.StepID, entry.Call.Name)
			rec.Input = obs.NormalizeMap(entry.Call.Input)
		}
		if entry.Result.Name != "" || entry.Result.Result != nil || entry.Result.Error != "" {
			rec := ensure(entry.Result.ID, entry.StepID, entry.Result.Name)
			if entry.Result.Result != nil {
				rec.Result = obs.NormalizeValue(entry.Result.Result)
			}
			if entry.Result.Error != "" {
				rec.Error = entry.Result.Error
			}
		}
	}

	out := make([]obs.ToolCallRecord, 0, len(order))
	for _, key := range order {
		rec := byKey[key]
		if rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Step == out[j].Step {
			return out[i].Name < out[j].Name
		}
		return out[i].Step < out[j].Step
	})
	return out
}

func mustJSON(v any) string {
	if v == nil {
		return "null"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<unserializable: %v>", err)
	}
	return string(data)
}

func (c *conversation) pickModel() error {
	options := c.providerOptions()
	if len(options) == 0 {
		fmt.Println()
		fmt.Println("No providers available.")
		fmt.Println()
		return nil
	}

	if !isTerminal(int(os.Stdin.Fd())) {
		return c.pickModelNumeric(options)
	}

	fd := int(os.Stdin.Fd())
	state, err := enableRaw(fd)
	if err != nil {
		return c.pickModelNumeric(options)
	}
	defer restoreTerm(fd, state)

	hideCursor()
	defer showCursor()

	fmt.Println()
	fmt.Println("Select provider/model (↑/↓ then Enter, q to cancel):")

	index := c.currentOptionIndex(options)
	renderModelMenu(options, index, true)

	for {
		b, err := readByte()
		if err != nil {
			fmt.Println()
			return err
		}
		switch b {
		case '\r', '\n':
			clearModelMenu(len(options))
			fmt.Println()
			return c.applyModelSelection(options[index])
		case 'q', 'Q':
			clearModelMenu(len(options))
			fmt.Println("Selection cancelled.")
			fmt.Println()
			return nil
		case 27:
			next, err := readByte()
			if err != nil {
				clearModelMenu(len(options))
				fmt.Println()
				return err
			}
			if next != '[' {
				clearModelMenu(len(options))
				fmt.Println("Selection cancelled.")
				fmt.Println()
				return nil
			}
			arrow, err := readByte()
			if err != nil {
				clearModelMenu(len(options))
				fmt.Println()
				return err
			}
			switch arrow {
			case 'A':
				if index > 0 {
					index--
					renderModelMenu(options, index, false)
				}
			case 'B':
				if index < len(options)-1 {
					index++
					renderModelMenu(options, index, false)
				}
			}
		}
	}
}

func (c *conversation) providerOptions() []providerOption {
	options := make([]providerOption, 0)
	for _, p := range c.providers {
		models := p.Models
		if len(models) == 0 {
			models = []string{p.Default}
		}
		for _, m := range models {
			options = append(options, providerOption{entry: p, model: m})
		}
	}
	return options
}

func (c *conversation) currentOptionIndex(options []providerOption) int {
	for i, opt := range options {
		if opt.entry == c.current && opt.model == c.currentModel {
			return i
		}
	}
	return 0
}

func (c *conversation) applyModelSelection(opt providerOption) error {
	if c.current != opt.entry {
		c.current = opt.entry
		c.runner = newRunner(opt.entry.Client)
	}
	c.currentModel = opt.model
	fmt.Printf("Switched to %s (%s).\n\n", opt.entry.Label, opt.model)
	return nil
}

func (c *conversation) pickModelNumeric(options []providerOption) error {
	fmt.Println()
	fmt.Println("Available providers/models:")
	for i, opt := range options {
		fmt.Printf("%d) %s — %s\n", i+1, opt.entry.Label, opt.model)
	}
	fmt.Print("Select option (or blank to cancel): ")
	line, err := c.readLine()
	if err != nil {
		return err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		fmt.Println("Selection cancelled.")
		fmt.Println()
		return nil
	}
	index, err := strconv.Atoi(line)
	if err != nil || index < 1 || index > len(options) {
		return fmt.Errorf("invalid selection")
	}
	fmt.Println()
	return c.applyModelSelection(options[index-1])
}

func renderModelMenu(options []providerOption, index int, first bool) {
	if !first {
		fmt.Printf("\x1b[%dF", len(options))
	}
	for i, opt := range options {
		marker := " "
		if i == index {
			marker = ">"
		}
		fmt.Printf("\x1b[2K%s %s — %s\n", marker, opt.entry.Label, opt.model)
	}
}

func clearModelMenu(lines int) {
	fmt.Printf("\x1b[%dF\x1b[J", lines+1)
}

func readByte() (byte, error) {
	var buf [1]byte
	_, err := os.Stdin.Read(buf[:])
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

func hideCursor() {
	fmt.Print("\x1b[?25l")
}

func showCursor() {
	fmt.Print("\x1b[?25h")
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
			val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
		return nil
	}
	return os.ErrNotExist
}

func initObservability(ctx context.Context) (func(context.Context) error, error) {
	opts := obs.DefaultOptions()
	opts.ServiceName = "gai-cli"

	setExporterFromEnv(&opts)
	configureMetricsFromEnv(&opts)
	configureBraintrustFromEnv(&opts)

	if opts.Exporter == obs.ExporterNone && !opts.Braintrust.Enabled {
		fmt.Fprintln(os.Stderr, "GAI CLI: observability disabled (no exporter or Braintrust configured)")
		return func(context.Context) error { return nil }, nil
	}

	shutdown, err := obs.Init(ctx, opts)
	if err != nil {
		return func(context.Context) error { return nil }, fmt.Errorf("init observability: %w", err)
	}
	return shutdown, nil
}

func setExporterFromEnv(opts *obs.Options) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GAI_OBS_EXPORTER"))) {
	case "stdout":
		opts.Exporter = obs.ExporterStdout
	case "otlp":
		opts.Exporter = obs.ExporterOTLP
	case "none":
		opts.Exporter = obs.ExporterNone
	}

	if opts.Exporter == obs.ExporterOTLP {
		if endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); endpoint != "" {
			opts.Endpoint = endpoint
		}
		if strings.EqualFold(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"), "true") {
			opts.Insecure = true
		}
	}

	if opts.Exporter == obs.ExporterOTLP && opts.Endpoint == "" {
		// Require explicit endpoint; otherwise default to disabling exporter
		opts.Exporter = obs.ExporterNone
	}

	if opts.Exporter != obs.ExporterOTLP {
		opts.Endpoint = ""
		opts.Insecure = false
	}
}

func configureMetricsFromEnv(opts *obs.Options) {
	if strings.EqualFold(os.Getenv("GAI_OBS_DISABLE_METRICS"), "true") {
		opts.DisableMetrics = true
	}
	if ratio := strings.TrimSpace(os.Getenv("GAI_OBS_SAMPLE_RATIO")); ratio != "" {
		if v, err := strconv.ParseFloat(ratio, 64); err == nil && v > 0 && v <= 1 {
			opts.SampleRatio = v
		}
	}
}

func configureBraintrustFromEnv(opts *obs.Options) {
	key := strings.TrimSpace(os.Getenv("BRAINTRUST_API_KEY"))
	if key == "" {
		opts.Braintrust.Enabled = false
		return
	}
	proj := strings.TrimSpace(os.Getenv("BRAINTRUST_PROJECT_NAME"))
	projID := strings.TrimSpace(os.Getenv("BRAINTRUST_PROJECT_ID"))
	if proj == "" && projID == "" {
		fmt.Fprintln(os.Stderr, "GAI CLI: BRAINTRUST_API_KEY set without project; disabling Braintrust sink")
		opts.Braintrust.Enabled = false
		return
	}
	opts.Braintrust.Enabled = true
	opts.Braintrust.APIKey = key
	opts.Braintrust.Project = proj
	opts.Braintrust.ProjectID = projID
	opts.Braintrust.Dataset = strings.TrimSpace(os.Getenv("BRAINTRUST_DATASET"))
	if baseURL := strings.TrimSpace(os.Getenv("BRAINTRUST_BASE_URL")); baseURL != "" {
		opts.Braintrust.BaseURL = baseURL
	}
}

func buildProviders() ([]*providerEntry, error) {
	providers := make([]*providerEntry, 0, 4)

	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		client := openai.New(
			openai.WithAPIKey(key),
			openai.WithModel("gpt-4.1-mini"),
		)
		providers = append(providers, &providerEntry{
			Name:    "openai",
			Label:   "OpenAI Chat",
			Models:  []string{"gpt-4.1-mini", "gpt-4.1", "gpt-4o"},
			Default: "gpt-4.1-mini",
			Client:  client,
		})
		respClient := openairesponses.New(
			openairesponses.WithAPIKey(key),
			openairesponses.WithModel("o4-mini"),
		)
		providers = append(providers, &providerEntry{
			Name:    "openai-responses",
			Label:   "OpenAI Responses",
			Models:  []string{"o4-mini", "o4", "gpt-4.1-mini", "gpt-5-codex"},
			Default: "o4-mini",
			Client:  respClient,
		})
	}

	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		client := anthropic.New(
			anthropic.WithAPIKey(key),
			anthropic.WithModel("claude-3-7-sonnet-20250219"),
		)
		providers = append(providers, &providerEntry{
			Name:    "anthropic",
			Label:   "Anthropic",
			Models:  []string{"claude-3-7-sonnet-20250219", "claude-3-5-haiku-20241022"},
			Default: "claude-3-7-sonnet-20250219",
			Client:  client,
		})
	}

	if key := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY")); key != "" {
		client := gemini.New(
			gemini.WithAPIKey(key),
			gemini.WithModel("gemini-2.5-pro"),
		)
		providers = append(providers, &providerEntry{
			Name:    "gemini",
			Label:   "Gemini",
			Models:  []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"},
			Default: "gemini-2.5-pro",
			Client:  client,
		})
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers initialised; set API keys in environment")
	}
	return providers, nil
}

func newRunner(p core.Provider) *runner.Runner {
	return runner.New(
		p,
		runner.WithMaxParallel(4),
		runner.WithToolTimeout(10*time.Second),
		runner.WithOnToolError(runner.ToolErrorAppendAndContinue),
	)
}

func newCounterTool(conv *conversation) core.ToolHandle {
	type counterInput struct {
		Amount int `json:"amount,omitempty"` // Optional increment amount (defaults to 1)
	}
	type counterOutput struct {
		Count int `json:"count"`
	}
	tool := tools.New[counterInput, counterOutput](
		"increment_counter",
		"Increment and return the latest counter value.",
		func(ctx context.Context, in counterInput, meta core.ToolMeta) (counterOutput, error) {
			conv.counterMu.Lock()
			increment := in.Amount
			if increment == 0 {
				increment = 1
			}
			conv.counter += increment
			count := conv.counter
			conv.counterMu.Unlock()
			return counterOutput{Count: count}, nil
		},
	)
	return tools.NewCoreAdapter(tool)
}

package gai

import (
	"context"
	"errors"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/testutil"
	"github.com/shillcollin/gai/schema"
)

func TestNewClient(t *testing.T) {
	// Basic construction
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	// Check internal state
	if client.providers == nil {
		t.Error("providers map should be initialized")
	}
	if client.aliases == nil {
		t.Error("aliases map should be initialized")
	}
}

func TestClientWithProvider(t *testing.T) {
	mock := testutil.NewMockProvider()

	client := NewClient(
		WithProvider("mock", mock),
	)

	if !client.HasProvider("mock") {
		t.Error("client should have mock provider")
	}

	providers := client.Providers()
	found := false
	for _, p := range providers {
		if p == "mock" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Providers() should include 'mock'")
	}
}

func TestClientGenerate(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.SetTextResponse("Hello, World!")

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	result, err := client.Generate(ctx, Request("mock/test-model").User("Hi"))
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result.Text() != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result.Text())
	}

	// Verify the mock was called
	if len(mock.TextCalls) != 1 {
		t.Errorf("expected 1 text call, got %d", len(mock.TextCalls))
	}
}

func TestClientText(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.SetTextResponse("Simple response")

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	text, err := client.Text(ctx, Request("mock/model").User("Hello"))
	if err != nil {
		t.Fatalf("Text failed: %v", err)
	}

	if text != "Simple response" {
		t.Errorf("expected 'Simple response', got %q", text)
	}
}

func TestClientTextNoContent(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.TextResponse = &core.TextResult{
		Text:     "",
		Model:    "mock-model",
		Provider: "mock",
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	_, err := client.Text(ctx, Request("mock/model").User("Hello"))
	if err == nil {
		t.Error("expected error for empty text")
	}
	if !errors.Is(err, ErrNoText) {
		t.Errorf("expected ErrNoText, got %v", err)
	}
}

func TestClientUnmarshal(t *testing.T) {
	mock := testutil.NewMockProvider()
	err := mock.SetObjectResponse(map[string]string{"name": "Alice", "city": "NYC"})
	if err != nil {
		t.Fatalf("SetObjectResponse failed: %v", err)
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	var result struct {
		Name string `json:"name"`
		City string `json:"city"`
	}

	err = client.Unmarshal(ctx, Request("mock/model").User("Get info"), &result)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result.Name != "Alice" {
		t.Errorf("expected Name='Alice', got %q", result.Name)
	}
	if result.City != "NYC" {
		t.Errorf("expected City='NYC', got %q", result.City)
	}
}

func TestClientStream(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.SetTextResponse("Streamed text")

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	stream, err := client.Stream(ctx, Request("mock/model").User("Stream please"))
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close()

	text, err := stream.CollectText()
	if err != nil {
		t.Fatalf("CollectText failed: %v", err)
	}

	if text != "Streamed text" {
		t.Errorf("expected 'Streamed text', got %q", text)
	}
}

func TestClientResolveModelAlias(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.SetTextResponse("Resolved via alias")

	client := NewClient(
		WithProvider("mock", mock),
		WithAlias("fast", "mock/fast-model"),
	)

	ctx := context.Background()
	text, err := client.Text(ctx, Request("fast").User("Hello"))
	if err != nil {
		t.Fatalf("Text with alias failed: %v", err)
	}

	if text != "Resolved via alias" {
		t.Errorf("expected 'Resolved via alias', got %q", text)
	}

	// Verify the correct model was passed
	if len(mock.TextCalls) != 1 {
		t.Fatalf("expected 1 text call, got %d", len(mock.TextCalls))
	}
	if mock.TextCalls[0].Model != "fast-model" {
		t.Errorf("expected model 'fast-model', got %q", mock.TextCalls[0].Model)
	}
}

func TestClientResolveModelNoProvider(t *testing.T) {
	client := NewClient()

	ctx := context.Background()
	_, err := client.Generate(ctx, Request("unknown/model").User("Hello"))
	if err == nil {
		t.Error("expected error for unknown provider")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Errorf("expected ModelError, got %T", err)
	}
}

func TestClientResolveModelInvalidFormat(t *testing.T) {
	client := NewClient()

	ctx := context.Background()
	_, err := client.Generate(ctx, Request("no-slash").User("Hello"))
	if err == nil {
		t.Error("expected error for invalid model format")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Errorf("expected ModelError, got %T", err)
	}
}

func TestClientResolveModelEmpty(t *testing.T) {
	client := NewClient()

	ctx := context.Background()
	_, err := client.Generate(ctx, Request("").User("Hello"))
	if err == nil {
		t.Error("expected error for empty model")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Errorf("expected ModelError, got %T", err)
	}
}

func TestClientWithDefaultModel(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.SetTextResponse("Default model response")

	client := NewClient(
		WithProvider("mock", mock),
		WithDefaultModel("mock/default-model"),
	)

	ctx := context.Background()
	// Empty model should use default
	text, err := client.Text(ctx, Request("").User("Hello"))
	if err != nil {
		t.Fatalf("Text with default model failed: %v", err)
	}

	if text != "Default model response" {
		t.Errorf("expected 'Default model response', got %q", text)
	}

	// Verify the correct model was used
	if len(mock.TextCalls) != 1 {
		t.Fatalf("expected 1 text call, got %d", len(mock.TextCalls))
	}
	if mock.TextCalls[0].Model != "default-model" {
		t.Errorf("expected model 'default-model', got %q", mock.TextCalls[0].Model)
	}
}

func TestClientWithAliases(t *testing.T) {
	mock := testutil.NewMockProvider()

	client := NewClient(
		WithProvider("mock", mock),
		WithAliases(map[string]string{
			"fast":  "mock/fast-model",
			"smart": "mock/smart-model",
		}),
	)

	// Verify aliases are set
	aliases := client.Aliases()
	if aliases["fast"] != "mock/fast-model" {
		t.Errorf("expected fast alias, got %q", aliases["fast"])
	}
	if aliases["smart"] != "mock/smart-model" {
		t.Errorf("expected smart alias, got %q", aliases["smart"])
	}
}

func TestClientAliasModification(t *testing.T) {
	mock := testutil.NewMockProvider()

	client := NewClient(
		WithProvider("mock", mock),
	)

	// Set alias at runtime
	client.SetAlias("runtime", "mock/runtime-model")

	alias, ok := client.GetAlias("runtime")
	if !ok {
		t.Error("expected alias to be found")
	}
	if alias != "mock/runtime-model" {
		t.Errorf("expected 'mock/runtime-model', got %q", alias)
	}

	// Remove alias
	client.RemoveAlias("runtime")

	_, ok = client.GetAlias("runtime")
	if ok {
		t.Error("expected alias to be removed")
	}
}

func TestClientWithDefaultTemperature(t *testing.T) {
	mock := testutil.NewMockProvider()

	client := NewClient(
		WithProvider("mock", mock),
		WithDefaultTemperature(0.5),
	)

	ctx := context.Background()
	_, _ = client.Text(ctx, Request("mock/model").User("Hello"))

	if len(mock.TextCalls) != 1 {
		t.Fatalf("expected 1 text call, got %d", len(mock.TextCalls))
	}

	if mock.TextCalls[0].Temperature == 0 {
		t.Fatal("expected Temperature to be set")
	}
	if mock.TextCalls[0].Temperature != 0.5 {
		t.Errorf("expected Temperature=0.5, got %f", mock.TextCalls[0].Temperature)
	}
}

func TestClientWithDefaultMaxTokens(t *testing.T) {
	mock := testutil.NewMockProvider()

	client := NewClient(
		WithProvider("mock", mock),
		WithDefaultMaxTokens(1000),
	)

	ctx := context.Background()
	_, _ = client.Text(ctx, Request("mock/model").User("Hello"))

	if len(mock.TextCalls) != 1 {
		t.Fatalf("expected 1 text call, got %d", len(mock.TextCalls))
	}

	if mock.TextCalls[0].MaxTokens == 0 {
		t.Fatal("expected MaxTokens to be set")
	}
	if mock.TextCalls[0].MaxTokens != 1000 {
		t.Errorf("expected MaxTokens=1000, got %d", mock.TextCalls[0].MaxTokens)
	}
}

func TestClientProviderError(t *testing.T) {
	mock := testutil.NewMockProvider()
	mock.TextErr = errors.New("provider error")

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	_, err := client.Text(ctx, Request("mock/model").User("Hello"))
	if err == nil {
		t.Error("expected error from provider")
	}

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Errorf("expected ProviderError, got %T", err)
	}
}

func TestRequestBuilderChaining(t *testing.T) {
	// Test that all builder methods return *RequestBuilder for chaining
	req := Request("mock/model").
		System("You are helpful").
		User("Hello").
		Temperature(0.7).
		MaxTokens(500).
		TopP(0.9)

	if req.model != "mock/model" {
		t.Errorf("expected model 'mock/model', got %q", req.model)
	}
	if len(req.messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(req.messages))
	}
}

// mockTool is a simple tool implementation for testing
type mockTool struct {
	name        string
	description string
	execFn      func(ctx context.Context, input map[string]any) (any, error)
	execCount   int
}

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return t.description }
func (t *mockTool) InputSchema() *schema.Schema {
	return &schema.Schema{Type: "object"}
}
func (t *mockTool) OutputSchema() *schema.Schema { return nil }
func (t *mockTool) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	t.execCount++
	if t.execFn != nil {
		return t.execFn(ctx, input)
	}
	return "mock result", nil
}

func TestClientRun(t *testing.T) {
	// Configure a multi-step mock provider
	steps := []testutil.StepConfig{
		{
			Text: "I'll search for that.",
			ToolCalls: []core.ToolCall{
				{ID: "call-1", Name: "search", Input: map[string]any{"query": "test"}},
			},
			Usage: core.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		},
		{
			Text:  "Based on the search, here's your answer.",
			Usage: core.Usage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
		},
	}
	mock := testutil.NewMultiStepMockProvider(steps)

	tool := &mockTool{
		name:        "search",
		description: "Search for information",
		execFn: func(ctx context.Context, input map[string]any) (any, error) {
			return "search results here", nil
		},
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	result, err := client.Run(ctx,
		Request("mock/model").
			User("Search for test").
			Tools(tool).
			StopWhen(NoMoreTools()),
	)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify the result
	if !result.HasText() {
		t.Error("expected result to have text")
	}

	// Verify tool was called
	if tool.execCount != 1 {
		t.Errorf("expected tool to be called once, got %d", tool.execCount)
	}

	// Verify steps
	if result.StepCount() < 1 {
		t.Errorf("expected at least 1 step, got %d", result.StepCount())
	}
}

func TestClientRunMaxSteps(t *testing.T) {
	// Configure a mock that always returns tool calls
	steps := []testutil.StepConfig{
		{
			Text: "Step 1",
			ToolCalls: []core.ToolCall{
				{ID: "call-1", Name: "search", Input: map[string]any{"query": "1"}},
			},
			Usage: core.Usage{TotalTokens: 10},
		},
		{
			Text: "Step 2",
			ToolCalls: []core.ToolCall{
				{ID: "call-2", Name: "search", Input: map[string]any{"query": "2"}},
			},
			Usage: core.Usage{TotalTokens: 10},
		},
		{
			Text: "Step 3",
			ToolCalls: []core.ToolCall{
				{ID: "call-3", Name: "search", Input: map[string]any{"query": "3"}},
			},
			Usage: core.Usage{TotalTokens: 10},
		},
	}
	mock := testutil.NewMultiStepMockProvider(steps)

	tool := &mockTool{
		name:        "search",
		description: "Search for information",
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	result, err := client.Run(ctx,
		Request("mock/model").
			User("Search").
			Tools(tool).
			MaxSteps(2),
	)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should stop at 2 steps
	if result.StepCount() > 2 {
		t.Errorf("expected at most 2 steps, got %d", result.StepCount())
	}

	// Verify finish reason
	reason := result.FinishReason()
	if reason.Type != StopReasonMaxSteps {
		t.Errorf("expected StopReasonMaxSteps, got %s", reason.Type)
	}
}

func TestClientRunNoMoreTools(t *testing.T) {
	// Configure a mock that returns text without tool calls
	steps := []testutil.StepConfig{
		{
			Text:  "Here's your answer directly.",
			Usage: core.Usage{TotalTokens: 20},
		},
	}
	mock := testutil.NewMultiStepMockProvider(steps)

	tool := &mockTool{
		name:        "search",
		description: "Search for information",
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	result, err := client.Run(ctx,
		Request("mock/model").
			User("Simple question").
			Tools(tool).
			StopWhen(NoMoreTools()),
	)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify tool was NOT called
	if tool.execCount != 0 {
		t.Errorf("expected tool to not be called, got %d calls", tool.execCount)
	}

	// Verify finish reason
	reason := result.FinishReason()
	if reason.Type != StopReasonNoMoreTools {
		t.Errorf("expected StopReasonNoMoreTools, got %s", reason.Type)
	}
}

func TestClientStreamRun(t *testing.T) {
	// Configure a multi-step mock provider
	steps := []testutil.StepConfig{
		{
			Text: "Thinking...",
			ToolCalls: []core.ToolCall{
				{ID: "call-1", Name: "calculate", Input: map[string]any{"expr": "2+2"}},
			},
			Usage: core.Usage{TotalTokens: 10},
		},
		{
			Text:  "The answer is 4.",
			Usage: core.Usage{TotalTokens: 15},
		},
	}
	mock := testutil.NewMultiStepMockProvider(steps)

	tool := &mockTool{
		name:        "calculate",
		description: "Calculate expression",
		execFn: func(ctx context.Context, input map[string]any) (any, error) {
			return 4, nil
		},
	}

	client := NewClient(
		WithProvider("mock", mock),
	)

	ctx := context.Background()
	stream, err := client.StreamRun(ctx,
		Request("mock/model").
			User("What is 2+2?").
			Tools(tool).
			StopWhen(NoMoreTools()),
	)
	if err != nil {
		t.Fatalf("StreamRun failed: %v", err)
	}
	defer stream.Close()

	// Collect events
	var textChunks []string
	var toolCalls []core.ToolCall
	for event := range stream.Events() {
		switch event.Type {
		case EventTextDelta:
			textChunks = append(textChunks, event.TextDelta)
		case EventToolCall:
			toolCalls = append(toolCalls, event.ToolCall)
		}
	}

	if err := stream.Err(); err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	// Verify we got text
	if len(textChunks) == 0 {
		t.Error("expected text events")
	}

	// Verify we got tool call events
	if len(toolCalls) == 0 {
		t.Error("expected tool call events")
	}
}

func TestClientRunWithCustomStopCondition(t *testing.T) {
	// Configure mock with tool calls so the loop continues past first step
	// The custom condition will fire after the first step completes
	tool := &mockTool{
		name:        "search",
		description: "Search for info",
	}

	steps := []testutil.StepConfig{
		{
			Text: "Step 1",
			ToolCalls: []core.ToolCall{
				{ID: "call-1", Name: "search", Input: map[string]any{"q": "test"}},
			},
			Usage: core.Usage{TotalTokens: 50},
		},
		{Text: "Step 2", Usage: core.Usage{TotalTokens: 60}},
	}
	mock := testutil.NewMultiStepMockProvider(steps)

	client := NewClient(
		WithProvider("mock", mock),
	)

	// Custom condition: stop after we have at least 1 completed step
	customCond := Custom(func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		// Stop after first step is completed
		if state.TotalSteps() >= 1 {
			return true, StopReason{
				Type:        "custom_after_step_1",
				Description: "Custom condition triggered after step 1",
			}
		}
		return false, StopReason{}
	})

	ctx := context.Background()
	result, err := client.Run(ctx,
		Request("mock/model").
			User("Test").
			Tools(tool).
			StopWhen(customCond),
	)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	reason := result.FinishReason()
	if reason.Type != "custom_after_step_1" {
		t.Errorf("expected custom_after_step_1 reason type, got %s", reason.Type)
	}
}

func TestRunnerTypesReexport(t *testing.T) {
	// Test that runner types are accessible at gai package level
	var _ ToolRetry
	var _ ToolMemo
	var _ Interceptor
	var _ ToolErrorMode
	var _ FinalState
	var _ Finalizer
	var _ Step
	var _ ToolExecution
	var _ ToolMeta

	// Test constants
	if ToolErrorPropagate == ToolErrorAppendAndContinue {
		t.Error("ToolErrorMode constants should be different")
	}
}

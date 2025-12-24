package gai

import (
	"testing"

	"github.com/shillcollin/gai/core"
)

func TestResultText(t *testing.T) {
	result := newResult(&core.TextResult{
		Text:     "Hello, World!",
		Model:    "test-model",
		Provider: "test-provider",
	})

	if result.Text() != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result.Text())
	}
}

func TestResultTextEmpty(t *testing.T) {
	result := newResult(&core.TextResult{
		Text: "",
	})

	if result.Text() != "" {
		t.Errorf("expected empty string, got %q", result.Text())
	}
}

func TestResultTextNil(t *testing.T) {
	result := newResult(nil)

	if result.Text() != "" {
		t.Errorf("expected empty string for nil result, got %q", result.Text())
	}
}

func TestResultHasText(t *testing.T) {
	withText := newResult(&core.TextResult{Text: "content"})
	withoutText := newResult(&core.TextResult{Text: ""})
	nilResult := newResult(nil)

	if !withText.HasText() {
		t.Error("expected HasText() to be true when text is present")
	}
	if withoutText.HasText() {
		t.Error("expected HasText() to be false when text is empty")
	}
	if nilResult.HasText() {
		t.Error("expected HasText() to be false when result is nil")
	}
}

func TestResultModel(t *testing.T) {
	result := newResult(&core.TextResult{
		Model: "gpt-4o",
	})

	if result.Model() != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got %q", result.Model())
	}
}

func TestResultModelNil(t *testing.T) {
	result := newResult(nil)

	if result.Model() != "" {
		t.Errorf("expected empty string for nil result, got %q", result.Model())
	}
}

func TestResultProvider(t *testing.T) {
	result := newResult(&core.TextResult{
		Provider: "openai",
	})

	if result.Provider() != "openai" {
		t.Errorf("expected 'openai', got %q", result.Provider())
	}
}

func TestResultUsage(t *testing.T) {
	result := newResult(&core.TextResult{
		Usage: core.Usage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	})

	usage := result.Usage()
	if usage.InputTokens != 100 {
		t.Errorf("expected InputTokens=100, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("expected OutputTokens=50, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("expected TotalTokens=150, got %d", usage.TotalTokens)
	}
}

func TestResultTokenAccessors(t *testing.T) {
	result := newResult(&core.TextResult{
		Usage: core.Usage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	})

	if result.InputTokens() != 100 {
		t.Errorf("expected InputTokens()=100, got %d", result.InputTokens())
	}
	if result.OutputTokens() != 50 {
		t.Errorf("expected OutputTokens()=50, got %d", result.OutputTokens())
	}
	if result.TotalTokens() != 150 {
		t.Errorf("expected TotalTokens()=150, got %d", result.TotalTokens())
	}
}

func TestResultUsageNil(t *testing.T) {
	result := newResult(nil)

	usage := result.Usage()
	if usage.TotalTokens != 0 {
		t.Errorf("expected TotalTokens=0 for nil result, got %d", usage.TotalTokens)
	}
}

func TestResultSteps(t *testing.T) {
	steps := []core.Step{
		{Number: 1},
		{Number: 2},
	}
	result := newResult(&core.TextResult{
		Steps: steps,
	})

	if len(result.Steps()) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps()))
	}
	if result.StepCount() != 2 {
		t.Errorf("expected StepCount()=2, got %d", result.StepCount())
	}
}

func TestResultStepsNil(t *testing.T) {
	result := newResult(nil)

	if result.Steps() != nil {
		t.Error("expected nil steps for nil result")
	}
	if result.StepCount() != 0 {
		t.Errorf("expected StepCount()=0 for nil result, got %d", result.StepCount())
	}
}

func TestResultToolCalls(t *testing.T) {
	result := newResult(&core.TextResult{
		Steps: []core.Step{
			{
				Number: 1,
				ToolCalls: []core.ToolExecution{
					{
						Call: core.ToolCall{
							ID:   "call-1",
							Name: "calculator",
						},
					},
					{
						Call: core.ToolCall{
							ID:   "call-2",
							Name: "search",
						},
					},
				},
			},
			{
				Number: 2,
				ToolCalls: []core.ToolExecution{
					{
						Call: core.ToolCall{
							ID:   "call-3",
							Name: "summarize",
						},
					},
				},
			},
		},
	})

	calls := result.ToolCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "calculator" {
		t.Errorf("expected first call to be 'calculator', got %q", calls[0].Name)
	}
	if calls[2].Name != "summarize" {
		t.Errorf("expected third call to be 'summarize', got %q", calls[2].Name)
	}

	// Test caching
	calls2 := result.ToolCalls()
	if len(calls2) != len(calls) {
		t.Error("cached tool calls should match original")
	}
}

func TestResultHasToolCalls(t *testing.T) {
	withCalls := newResult(&core.TextResult{
		Steps: []core.Step{
			{
				ToolCalls: []core.ToolExecution{
					{Call: core.ToolCall{Name: "tool"}},
				},
			},
		},
	})
	withoutCalls := newResult(&core.TextResult{
		Steps: []core.Step{{ToolCalls: nil}},
	})
	nilResult := newResult(nil)

	if !withCalls.HasToolCalls() {
		t.Error("expected HasToolCalls() to be true when tool calls present")
	}
	if withoutCalls.HasToolCalls() {
		t.Error("expected HasToolCalls() to be false when no tool calls")
	}
	if nilResult.HasToolCalls() {
		t.Error("expected HasToolCalls() to be false for nil result")
	}
}

func TestResultFinishReason(t *testing.T) {
	result := newResult(&core.TextResult{
		FinishReason: core.StopReason{
			Type:        core.StopReasonComplete,
			Description: "Task completed",
		},
	})

	reason := result.FinishReason()
	if reason.Type != core.StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", reason.Type)
	}
	if reason.Description != "Task completed" {
		t.Errorf("expected 'Task completed', got %q", reason.Description)
	}
}

func TestResultFinishReasonNil(t *testing.T) {
	result := newResult(nil)

	reason := result.FinishReason()
	if reason.Type != "" {
		t.Errorf("expected empty Type for nil result, got %q", reason.Type)
	}
}

func TestResultWarnings(t *testing.T) {
	result := newResult(&core.TextResult{
		Warnings: []core.Warning{
			{Code: "max_tokens", Message: "Truncated"},
			{Code: "param_dropped", Message: "Unknown parameter"},
		},
	})

	warnings := result.Warnings()
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}
	if warnings[0].Code != "max_tokens" {
		t.Errorf("expected first warning code 'max_tokens', got %q", warnings[0].Code)
	}
}

func TestResultHasWarnings(t *testing.T) {
	withWarnings := newResult(&core.TextResult{
		Warnings: []core.Warning{{Code: "warn"}},
	})
	withoutWarnings := newResult(&core.TextResult{
		Warnings: nil,
	})

	if !withWarnings.HasWarnings() {
		t.Error("expected HasWarnings() to be true when warnings present")
	}
	if withoutWarnings.HasWarnings() {
		t.Error("expected HasWarnings() to be false when no warnings")
	}
}

func TestResultCitations(t *testing.T) {
	result := newResult(&core.TextResult{
		Citations: []core.Citation{
			{URI: "https://example.com", Title: "Example"},
		},
	})

	citations := result.Citations()
	if len(citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(citations))
	}
	if citations[0].URI != "https://example.com" {
		t.Errorf("expected URI 'https://example.com', got %q", citations[0].URI)
	}
}

func TestResultLatency(t *testing.T) {
	result := newResult(&core.TextResult{
		LatencyMS: 250,
		TTFBMS:    50,
	})

	if result.LatencyMS() != 250 {
		t.Errorf("expected LatencyMS=250, got %d", result.LatencyMS())
	}
	if result.TTFBMS() != 50 {
		t.Errorf("expected TTFBMS=50, got %d", result.TTFBMS())
	}
}

func TestResultCore(t *testing.T) {
	inner := &core.TextResult{
		Text: "test",
	}
	result := newResult(inner)

	if result.Core() != inner {
		t.Error("Core() should return the underlying core.TextResult")
	}
}

func TestResultCoreNil(t *testing.T) {
	result := newResult(nil)

	if result.Core() != nil {
		t.Error("Core() should return nil when result is nil")
	}
}

func TestResultMessages(t *testing.T) {
	// Simple text response - no steps
	result := newResult(&core.TextResult{
		Text: "Hello, world!",
	})

	messages := result.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != core.Assistant {
		t.Errorf("expected Assistant role, got %s", messages[0].Role)
	}
	if len(messages[0].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(messages[0].Parts))
	}
	textPart, ok := messages[0].Parts[0].(core.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", messages[0].Parts[0])
	}
	if textPart.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", textPart.Text)
	}
}

func TestResultMessagesEmpty(t *testing.T) {
	// Empty text - should return nil
	result := newResult(&core.TextResult{
		Text: "",
	})

	messages := result.Messages()
	if messages != nil {
		t.Errorf("expected nil messages for empty text, got %d messages", len(messages))
	}
}

func TestResultMessagesNil(t *testing.T) {
	result := newResult(nil)

	messages := result.Messages()
	if messages != nil {
		t.Error("expected nil messages for nil result")
	}
}

func TestResultMessagesWithSteps(t *testing.T) {
	// Multi-step result with tool calls
	result := newResult(&core.TextResult{
		Text: "Final answer",
		Steps: []core.Step{
			{
				Number: 1,
				Text:   "Let me search for that.",
				ToolCalls: []core.ToolExecution{
					{
						Call: core.ToolCall{
							ID:    "call-1",
							Name:  "search",
							Input: map[string]any{"query": "test"},
						},
						Result: "Search results here",
						Error:  nil,
					},
				},
			},
			{
				Number: 2,
				Text:   "Based on the results, here's my answer.",
			},
		},
	})

	messages := result.Messages()

	// Should have: assistant (text + tool call), user (tool result), assistant (text)
	if len(messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(messages))
	}

	// First message should be assistant with text and tool call
	if messages[0].Role != core.Assistant {
		t.Errorf("first message should be Assistant, got %s", messages[0].Role)
	}

	// Second message should be tool result (as user message)
	if messages[1].Role != core.User {
		t.Errorf("second message should be User (tool result), got %s", messages[1].Role)
	}

	// Find the tool result part
	foundToolResult := false
	for _, part := range messages[1].Parts {
		if tr, ok := part.(core.ToolResult); ok {
			foundToolResult = true
			if tr.ID != "call-1" {
				t.Errorf("expected tool call ID 'call-1', got %q", tr.ID)
			}
			if tr.Name != "search" {
				t.Errorf("expected tool name 'search', got %q", tr.Name)
			}
		}
	}
	if !foundToolResult {
		t.Error("expected to find ToolResult part in second message")
	}
}

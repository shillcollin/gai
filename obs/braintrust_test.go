package obs

import (
	"encoding/json"
	"testing"
)

func TestCompletionToProjectLogEvent(t *testing.T) {
	completion := Completion{
		Provider:     "openai-responses",
		Model:        "gpt-5-codex",
		RequestID:    "req_123",
		Input:        []Message{{Role: "user", Text: "hello"}},
		Output:       Message{Role: "assistant", Text: "world"},
		Usage:        UsageTokens{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		LatencyMS:    123,
		Metadata:     map[string]any{"mode": "stream"},
		ToolCalls:    []ToolCallRecord{{Step: 1, ID: "tool_1", Name: "make_sheet"}},
		CreatedAtUTC: 1000,
	}

	event := completionToProjectLogEvent(completion)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal project event: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("unmarshal project event: %v", err)
	}

	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata map, got %T", doc["metadata"])
	}
	if metadata["provider"] != "openai-responses" {
		t.Fatalf("provider not propagated: %#v", metadata)
	}
	if metadata["request_id"] != "req_123" {
		t.Fatalf("request id missing: %#v", metadata)
	}
	if _, ok := metadata["tool_calls"].([]any); !ok {
		t.Fatalf("tool calls missing: %#v", metadata)
	}

	metrics, ok := doc["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics map, got %T", doc["metrics"])
	}
	if metrics["prompt_tokens"].(float64) != 10 {
		t.Fatalf("prompt tokens missing: %#v", metrics)
	}
	if metrics["completion_tokens"].(float64) != 5 {
		t.Fatalf("completion tokens missing: %#v", metrics)
	}
}

func TestCompletionToDatasetEvent(t *testing.T) {
	completion := Completion{
		Provider:     "openai-responses",
		Model:        "gpt-5-codex",
		RequestID:    "req_456",
		Input:        []Message{{Role: "user", Text: "hello"}},
		Output:       Message{Role: "assistant", Text: "world"},
		Metadata:     map[string]any{"mode": "json"},
		ToolCalls:    []ToolCallRecord{{Step: 2, ID: "tool_9", Name: "audit"}},
		CreatedAtUTC: 2000,
	}

	event := completionToDatasetEvent(completion)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal dataset event: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("unmarshal dataset event: %v", err)
	}

	input, ok := doc["input"].(map[string]any)
	if !ok {
		t.Fatalf("expected input map, got %T", doc["input"])
	}
	if input["provider"] != "openai-responses" {
		t.Fatalf("provider missing: %#v", input)
	}
	if input["model"] != "gpt-5-codex" {
		t.Fatalf("model missing: %#v", input)
	}

	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata map, got %T", doc["metadata"])
	}
	if metadata["mode"] != "json" {
		t.Fatalf("metadata not preserved: %#v", metadata)
	}
	if _, ok := metadata["tool_calls"].([]any); !ok {
		t.Fatalf("tool calls missing: %#v", metadata)
	}
}

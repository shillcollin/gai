//go:build integration
// +build integration

package integration_test

import (
	"strings"
	"testing"
	"time"

	vango "github.com/vango-ai/vango/sdk"
)

func TestMessages_Stream_SimpleText(t *testing.T) {
	requireAnthropicKey(t)
	ctx := defaultTestContext(t)

	stream, err := testClient.Messages.Stream(ctx, &vango.MessageRequest{
		Model: "anthropic/claude-haiku-4-5-20251001",
		Messages: []vango.Message{
			{Role: "user", Content: vango.Text("Count from 1 to 5, one number per line.")},
		},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var events []vango.StreamEvent
	var textContent strings.Builder
	var gotMessageStart, gotMessageStop bool

	for event := range stream.Events() {
		events = append(events, event)

		switch e := event.(type) {
		case vango.MessageStartEvent:
			gotMessageStart = true
			if e.Message.ID == "" {
				t.Error("expected non-empty message ID in message_start")
			}
		case vango.ContentBlockDeltaEvent:
			if delta, ok := e.Delta.(vango.TextDelta); ok {
				textContent.WriteString(delta.Text)
			}
		case vango.MessageStopEvent:
			gotMessageStop = true
		}
	}

	if !gotMessageStart {
		t.Error("expected message_start event")
	}
	if !gotMessageStop {
		t.Error("expected message_stop event")
	}

	text := textContent.String()
	if !strings.Contains(text, "1") {
		t.Errorf("expected '1' in streamed text, got %q", text)
	}
	if !strings.Contains(text, "5") {
		t.Errorf("expected '5' in streamed text, got %q", text)
	}

	// Verify final response matches accumulated text
	resp := stream.Response()
	if resp == nil {
		t.Fatal("expected non-nil response after stream")
	}
	if resp.TextContent() != text {
		t.Errorf("accumulated text mismatch:\nGot: %q\nWant: %q", text, resp.TextContent())
	}

	// EOF is normal for stream completion
	if err := stream.Err(); err != nil && err.Error() != "EOF" {
		t.Errorf("unexpected stream error: %v", err)
	}
}

func TestMessages_Stream_EventOrder(t *testing.T) {
	requireAnthropicKey(t)
	ctx := defaultTestContext(t)

	stream, err := testClient.Messages.Stream(ctx, &vango.MessageRequest{
		Model: "anthropic/claude-haiku-4-5-20251001",
		Messages: []vango.Message{
			{Role: "user", Content: vango.Text("Hi")},
		},
		MaxTokens: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	// Collect event types in order
	var eventTypes []string
	for event := range stream.Events() {
		switch event.(type) {
		case vango.MessageStartEvent:
			eventTypes = append(eventTypes, "message_start")
		case vango.ContentBlockStartEvent:
			eventTypes = append(eventTypes, "content_block_start")
		case vango.ContentBlockDeltaEvent:
			eventTypes = append(eventTypes, "content_block_delta")
		case vango.ContentBlockStopEvent:
			eventTypes = append(eventTypes, "content_block_stop")
		case vango.MessageDeltaEvent:
			eventTypes = append(eventTypes, "message_delta")
		case vango.MessageStopEvent:
			eventTypes = append(eventTypes, "message_stop")
		case vango.PingEvent:
			eventTypes = append(eventTypes, "ping")
		}
	}

	// Verify expected order
	if len(eventTypes) < 4 {
		t.Fatalf("expected at least 4 events, got %d: %v", len(eventTypes), eventTypes)
	}
	if eventTypes[0] != "message_start" {
		t.Errorf("expected first event to be message_start, got %s", eventTypes[0])
	}
	if eventTypes[1] != "content_block_start" {
		t.Errorf("expected second event to be content_block_start, got %s", eventTypes[1])
	}
	if eventTypes[len(eventTypes)-1] != "message_stop" {
		t.Errorf("expected last event to be message_stop, got %s", eventTypes[len(eventTypes)-1])
	}
}

func TestMessages_Stream_LongResponse(t *testing.T) {
	requireAnthropicKey(t)
	ctx := testContext(t, 120*time.Second)

	stream, err := testClient.Messages.Stream(ctx, &vango.MessageRequest{
		Model: "anthropic/claude-haiku-4-5-20251001",
		Messages: []vango.Message{
			{Role: "user", Content: vango.Text("Write a detailed 500 word essay about space exploration.")},
		},
		MaxTokens: 1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	startTime := time.Now()
	var firstDeltaTime time.Duration
	var deltaCount int

	for event := range stream.Events() {
		if _, ok := event.(vango.ContentBlockDeltaEvent); ok {
			if deltaCount == 0 {
				firstDeltaTime = time.Since(startTime)
			}
			deltaCount++
		}
	}

	// Time to first token should be reasonable
	if firstDeltaTime > 10*time.Second {
		t.Errorf("time to first token too slow: %v", firstDeltaTime)
	}

	// Should have many delta events for a long response
	if deltaCount < 20 {
		t.Errorf("expected many delta events for long response, got %d", deltaCount)
	}

	t.Logf("Time to first token: %v, total deltas: %d", firstDeltaTime, deltaCount)
}

func TestMessages_Stream_Cancel(t *testing.T) {
	requireAnthropicKey(t)
	ctx := defaultTestContext(t)

	stream, err := testClient.Messages.Stream(ctx, &vango.MessageRequest{
		Model: "anthropic/claude-haiku-4-5-20251001",
		Messages: []vango.Message{
			{Role: "user", Content: vango.Text("Write a very long story about a magical kingdom.")},
		},
		MaxTokens: 2000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read a few events then cancel
	eventCount := 0
	for range stream.Events() {
		eventCount++
		if eventCount >= 5 {
			break
		}
	}

	// Close the stream
	if err := stream.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestMessages_Stream_TextAccumulation(t *testing.T) {
	requireAnthropicKey(t)
	ctx := defaultTestContext(t)

	stream, err := testClient.Messages.Stream(ctx, &vango.MessageRequest{
		Model: "anthropic/claude-haiku-4-5-20251001",
		Messages: []vango.Message{
			{Role: "user", Content: vango.Text("Say 'Hello World' and nothing else.")},
		},
		MaxTokens: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var accumulated strings.Builder
	for event := range stream.Events() {
		if delta, ok := event.(vango.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.(vango.TextDelta); ok {
				accumulated.WriteString(text.Text)
			}
		}
	}

	resp := stream.Response()
	if resp == nil {
		t.Fatal("expected response after stream")
	}

	// The accumulated text should match the response text
	if accumulated.String() != resp.TextContent() {
		t.Errorf("text mismatch:\nAccumulated: %q\nResponse: %q", accumulated.String(), resp.TextContent())
	}

	// Should contain "Hello"
	if !strings.Contains(accumulated.String(), "Hello") {
		t.Errorf("expected 'Hello' in text, got %q", accumulated.String())
	}
}

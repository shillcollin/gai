package gai

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/internal/testutil"
	"github.com/shillcollin/gai/schema"
)

func TestConversationBasicSay(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Hello! How can I help you?")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults:  ClientDefaults{Model: "mock/test-model"},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvSystem("You are a helpful assistant"),
	)

	ctx := context.Background()
	result, err := conv.Say(ctx, "Hello!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text() != "Hello! How can I help you?" {
		t.Errorf("expected 'Hello! How can I help you?', got %q", result.Text())
	}

	// Check history was updated
	msgs := conv.Messages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages in history, got %d", len(msgs))
	}

	// First should be user message
	if msgs[0].Role != core.User {
		t.Errorf("expected first message to be user, got %s", msgs[0].Role)
	}

	// Second should be assistant
	if msgs[1].Role != core.Assistant {
		t.Errorf("expected second message to be assistant, got %s", msgs[1].Role)
	}
}

func TestConversationMultiTurn(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	// Track call count
	callCount := 0
	mockProvider.OnGenerateText = func(ctx context.Context, req core.Request) (*core.TextResult, error) {
		callCount++
		if callCount == 1 {
			return &core.TextResult{Text: "Hello! I'm here to help."}, nil
		}
		// Second call should include history
		if len(req.Messages) < 3 { // system (in request) + user + assistant + user
			t.Errorf("expected history in second request, got %d messages", len(req.Messages))
		}
		return &core.TextResult{Text: "I said: Hello! I'm here to help."}, nil
	}

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvSystem("You are a helpful assistant"),
	)

	ctx := context.Background()

	// First turn
	_, err := conv.Say(ctx, "Hello!")
	if err != nil {
		t.Fatalf("first turn error: %v", err)
	}

	// Second turn
	result, err := conv.Say(ctx, "What did you just say?")
	if err != nil {
		t.Fatalf("second turn error: %v", err)
	}

	if result.Text() != "I said: Hello! I'm here to help." {
		t.Errorf("unexpected response: %q", result.Text())
	}

	// Should have 4 messages: 2 user + 2 assistant
	if conv.MessageCount() != 4 {
		t.Errorf("expected 4 messages, got %d", conv.MessageCount())
	}
}

func TestConversationAudioInputConversion(t *testing.T) {
	// Test that Conversation correctly converts AudioInput to core.Message
	// Note: This test doesn't call Say() because that triggers the voice pipeline.
	// It tests the inputToMessage conversion directly.

	mockProvider := testutil.NewMockProvider()
	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))

	// Test that audio bytes are converted to proper message format
	audioData := []byte("fake audio data")
	audioInput := ConvAudio(audioData)

	if audioInput.MIME != "audio/wav" {
		t.Errorf("expected default MIME audio/wav, got %q", audioInput.MIME)
	}

	// Test ConvAudioWithMIME
	mp3Input := ConvAudioWithMIME(audioData, "audio/mp3")
	if mp3Input.MIME != "audio/mp3" {
		t.Errorf("expected MIME audio/mp3, got %q", mp3Input.MIME)
	}

	// Verify conversation can add messages directly (bypassing Generate)
	conv.AddMessages(core.UserMessage(core.Audio{
		Source: core.BlobRef{
			Kind:  core.BlobBytes,
			Bytes: audioData,
			MIME:  "audio/wav",
		},
	}))

	msgs := conv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// First message should contain the audio part
	hasAudio := false
	for _, part := range msgs[0].Parts {
		if _, ok := part.(core.Audio); ok {
			hasAudio = true
			break
		}
	}
	if !hasAudio {
		t.Error("expected audio part in first message")
	}
}

func TestConversationRollback(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))

	ctx := context.Background()

	// Add some messages
	conv.Say(ctx, "Message 1")
	conv.Say(ctx, "Message 2")
	conv.Say(ctx, "Message 3")

	// Should have 6 messages (3 user + 3 assistant)
	if conv.MessageCount() != 6 {
		t.Fatalf("expected 6 messages, got %d", conv.MessageCount())
	}

	// Rollback last 2 messages
	conv.Rollback(2)

	if conv.MessageCount() != 4 {
		t.Errorf("expected 4 messages after rollback, got %d", conv.MessageCount())
	}
}

func TestConversationClear(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))

	ctx := context.Background()
	conv.Say(ctx, "Hello")

	if conv.MessageCount() == 0 {
		t.Fatal("expected messages after Say")
	}

	conv.Clear()

	if conv.MessageCount() != 0 {
		t.Errorf("expected 0 messages after clear, got %d", conv.MessageCount())
	}
}

func TestConversationFork(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvSystem("System prompt"),
	)

	ctx := context.Background()
	conv.Say(ctx, "Hello")

	// Fork the conversation
	fork := conv.Fork()

	// Modify original
	conv.Say(ctx, "Original continues")

	// Modify fork
	fork.Say(ctx, "Fork diverges")

	// They should have different history lengths
	if conv.MessageCount() == fork.MessageCount() {
		// This could happen but histories should be different
		origMsgs := conv.Messages()
		forkMsgs := fork.Messages()

		// Last messages should be different
		origLast := origMsgs[len(origMsgs)-2]
		forkLast := forkMsgs[len(forkMsgs)-2]

		if origLast.Parts[0].(core.Text).Text == forkLast.Parts[0].(core.Text).Text {
			t.Error("expected fork and original to have different histories")
		}
	}

	// Fork should have same settings
	if fork.Model() != conv.Model() {
		t.Errorf("expected fork to have same model")
	}
	if fork.System() != conv.System() {
		t.Errorf("expected fork to have same system prompt")
	}
}

func TestConversationMaxMessages(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	// Set max to 4 messages
	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvMaxMessages(4),
	)

	ctx := context.Background()

	// Add more than max
	conv.Say(ctx, "Message 1")
	conv.Say(ctx, "Message 2")
	conv.Say(ctx, "Message 3")

	// Should be trimmed to 4
	if conv.MessageCount() != 4 {
		t.Errorf("expected 4 messages (max), got %d", conv.MessageCount())
	}

	// Most recent messages should be kept
	msgs := conv.Messages()
	// Last user message should be "Message 3"
	lastUserMsg := msgs[len(msgs)-2]
	if text, ok := lastUserMsg.Parts[0].(core.Text); ok {
		if text.Text != "Message 3" {
			t.Errorf("expected last user message to be 'Message 3', got %q", text.Text)
		}
	}
}

func TestConversationMaxMessagesPreservesSystem(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	// Set max to 3 messages and add system
	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvSystem("I am the system"),
		ConvMaxMessages(3),
	)

	// Manually add system message to history for testing
	conv.AddMessages(core.SystemMessage("I am the system"))

	ctx := context.Background()

	// Add messages
	conv.Say(ctx, "Message 1")
	conv.Say(ctx, "Message 2")

	// Check system message is preserved
	msgs := conv.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}

	// First should be system
	if msgs[0].Role != core.System {
		t.Errorf("expected system message to be preserved, got %s", msgs[0].Role)
	}
}

func TestConversationJSONRoundtrip(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvSystem("System prompt"),
		ConvVoice("elevenlabs/rachel"),
		ConvSTT("deepgram/nova-2"),
	)

	ctx := context.Background()
	conv.Say(ctx, "Hello!")

	// Serialize
	data, err := json.Marshal(conv)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Deserialize into new conversation
	conv2 := client.Conversation()
	if err := json.Unmarshal(data, conv2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Check settings were preserved
	if conv2.Model() != conv.Model() {
		t.Errorf("model mismatch: %q vs %q", conv2.Model(), conv.Model())
	}
	if conv2.System() != conv.System() {
		t.Errorf("system mismatch: %q vs %q", conv2.System(), conv.System())
	}

	// Check messages were preserved
	if conv2.MessageCount() != conv.MessageCount() {
		t.Errorf("message count mismatch: %d vs %d", conv2.MessageCount(), conv.MessageCount())
	}
}

func TestConversationThreadSafety(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))

	ctx := context.Background()

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			conv.Say(ctx, "Message")
		}(i)
	}

	wg.Wait()

	// Should have 20 messages (10 user + 10 assistant)
	if conv.MessageCount() != 20 {
		t.Errorf("expected 20 messages, got %d", conv.MessageCount())
	}
}

func TestConversationWithCallOptions(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
	)

	ctx := context.Background()

	// Test that call options work (using tools as example)
	mockTool := &conversationMockTool{name: "test_tool"}
	_, err := conv.Say(ctx, "Hello", WithCallTools(mockTool))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify message was added to history
	if conv.MessageCount() != 2 {
		t.Errorf("expected 2 messages, got %d", conv.MessageCount())
	}
}

// conversationMockTool is a simple mock for testing conversation calls
type conversationMockTool struct {
	name string
}

func (m *conversationMockTool) Name() string                     { return m.name }
func (m *conversationMockTool) Description() string              { return "Mock tool" }
func (m *conversationMockTool) InputSchema() *schema.Schema      { return nil }
func (m *conversationMockTool) OutputSchema() *schema.Schema     { return nil }
func (m *conversationMockTool) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	return "result", nil
}

func TestConversationInputTypes(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Response")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))
	ctx := context.Background()

	// Test string input
	_, err := conv.Say(ctx, "Hello string")
	if err != nil {
		t.Errorf("string input failed: %v", err)
	}

	// Test core.Message
	_, err = conv.Say(ctx, core.UserMessage(core.Text{Text: "Direct message"}))
	if err != nil {
		t.Errorf("core.Message input failed: %v", err)
	}

	// Test invalid type
	_, err = conv.Say(ctx, 12345)
	if err == nil {
		t.Error("expected error for invalid input type")
	}

	// Note: []byte and AudioInput are valid input types but trigger the voice pipeline
	// which requires STT configuration. Those paths are tested in voice_test.go.
}

func TestConversationStream(t *testing.T) {
	mockProvider := testutil.NewMockProvider()
	mockProvider.SetTextResponse("Once upon a time...")

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))
	ctx := context.Background()

	stream, err := conv.Stream(ctx, "Tell me a story")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consume events
	var text string
	for event := range stream.Events() {
		if event.Type == core.EventTextDelta {
			text += event.TextDelta
		}
	}

	if text == "" {
		t.Error("expected some text from stream")
	}

	// Close should update history
	stream.Close()

	// After close, history should be updated
	// Note: If collectResult finds the stream already consumed, it will have empty text
	// This is expected behavior - the caller should collect text during streaming
	if conv.MessageCount() < 2 {
		t.Errorf("expected at least 2 messages after stream, got %d", conv.MessageCount())
	}
}

func TestConversationAddMessages(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(ConvModel("mock/test-model"))

	// Manually add messages
	conv.AddMessages(
		core.UserMessage(core.Text{Text: "Hello"}),
		core.AssistantMessage("Hi there!"),
	)

	if conv.MessageCount() != 2 {
		t.Errorf("expected 2 messages, got %d", conv.MessageCount())
	}

	msgs := conv.Messages()
	if msgs[0].Role != core.User {
		t.Errorf("expected user message first")
	}
	if msgs[1].Role != core.Assistant {
		t.Errorf("expected assistant message second")
	}
}

func TestConversationDefaults(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
		defaults: ClientDefaults{
			Model: "mock/default-model",
			Voice: "default-voice",
			STT:   "default-stt",
		},
	}

	// Create conversation without specifying model
	conv := client.Conversation()

	// Should inherit client defaults
	if conv.Model() != "mock/default-model" {
		t.Errorf("expected model from defaults, got %q", conv.Model())
	}
}

func TestConversationMetadata(t *testing.T) {
	mockProvider := testutil.NewMockProvider()

	client := &Client{
		providers: map[string]core.Provider{"mock": mockProvider},
	}

	conv := client.Conversation(
		ConvModel("mock/test-model"),
		ConvMetadata("user_id", "123"),
		ConvMetadata("session", "abc"),
	)

	// Verify metadata via Fork (which copies it)
	fork := conv.Fork()
	_ = fork // Metadata is internal, but Fork should copy it
}

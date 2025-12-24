package gai

import (
	"context"
	"testing"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/schema"
)

func TestRequestBasic(t *testing.T) {
	req := Request("openai/gpt-4o")
	if req.model != "openai/gpt-4o" {
		t.Errorf("expected model 'openai/gpt-4o', got %q", req.model)
	}
	if len(req.messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(req.messages))
	}
}

func TestRequestSystem(t *testing.T) {
	req := Request("model").System("You are helpful")

	if len(req.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.messages))
	}
	if req.messages[0].Role != core.System {
		t.Errorf("expected system role, got %s", req.messages[0].Role)
	}
}

func TestRequestUser(t *testing.T) {
	req := Request("model").User("Hello")

	if len(req.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.messages))
	}
	if req.messages[0].Role != core.User {
		t.Errorf("expected user role, got %s", req.messages[0].Role)
	}
	if len(req.messages[0].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(req.messages[0].Parts))
	}
	text, ok := req.messages[0].Parts[0].(core.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", req.messages[0].Parts[0])
	}
	if text.Text != "Hello" {
		t.Errorf("expected 'Hello', got %q", text.Text)
	}
}

func TestRequestAssistant(t *testing.T) {
	req := Request("model").Assistant("I can help")

	if len(req.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.messages))
	}
	if req.messages[0].Role != core.Assistant {
		t.Errorf("expected assistant role, got %s", req.messages[0].Role)
	}
}

func TestRequestChaining(t *testing.T) {
	req := Request("model").
		System("System prompt").
		User("User message").
		Assistant("Assistant response").
		User("Follow-up")

	if len(req.messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(req.messages))
	}

	expectedRoles := []core.Role{core.System, core.User, core.Assistant, core.User}
	for i, role := range expectedRoles {
		if req.messages[i].Role != role {
			t.Errorf("message %d: expected role %s, got %s", i, role, req.messages[i].Role)
		}
	}
}

func TestRequestTemperature(t *testing.T) {
	req := Request("model").Temperature(0.7)

	if req.temperature == nil {
		t.Fatal("expected temperature to be set")
	}
	if *req.temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", *req.temperature)
	}
}

func TestRequestMaxTokens(t *testing.T) {
	req := Request("model").MaxTokens(500)

	if req.maxTokens == nil {
		t.Fatal("expected maxTokens to be set")
	}
	if *req.maxTokens != 500 {
		t.Errorf("expected maxTokens 500, got %d", *req.maxTokens)
	}
}

func TestRequestTopP(t *testing.T) {
	req := Request("model").TopP(0.9)

	if req.topP == nil {
		t.Fatal("expected topP to be set")
	}
	if *req.topP != 0.9 {
		t.Errorf("expected topP 0.9, got %f", *req.topP)
	}
}

func TestRequestTopK(t *testing.T) {
	req := Request("model").TopK(40)

	if req.topK == nil {
		t.Fatal("expected topK to be set")
	}
	if *req.topK != 40 {
		t.Errorf("expected topK 40, got %d", *req.topK)
	}
}

func TestRequestMetadata(t *testing.T) {
	req := Request("model").
		Metadata("key1", "value1").
		Metadata("key2", 42)

	if req.metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if req.metadata["key1"] != "value1" {
		t.Errorf("expected key1='value1', got %v", req.metadata["key1"])
	}
	if req.metadata["key2"] != 42 {
		t.Errorf("expected key2=42, got %v", req.metadata["key2"])
	}
}

func TestRequestOption(t *testing.T) {
	req := Request("model").
		Option("custom", "value")

	if req.providerOpts == nil {
		t.Fatal("expected providerOpts to be set")
	}
	if req.providerOpts["custom"] != "value" {
		t.Errorf("expected custom='value', got %v", req.providerOpts["custom"])
	}
}

func TestRequestMessages(t *testing.T) {
	msgs := []core.Message{
		core.SystemMessage("System"),
		core.UserMessage(core.Text{Text: "User"}),
	}

	req := Request("model").Messages(msgs...)

	if len(req.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.messages))
	}
}

func TestRequestTools(t *testing.T) {
	tool := &mockToolHandle{name: "calculator"}

	req := Request("model").Tools(tool)

	if len(req.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.tools))
	}
}

func TestRequestToolChoice(t *testing.T) {
	req := Request("model").ToolChoice(core.ToolChoiceAuto)

	if req.toolChoice != core.ToolChoiceAuto {
		t.Errorf("expected ToolChoiceAuto, got %s", req.toolChoice)
	}
}

func TestRequestBuild(t *testing.T) {
	req := Request("provider/model").
		System("System prompt").
		User("Hello").
		Temperature(0.8).
		MaxTokens(100).
		Metadata("trace", "123")

	defaults := ClientDefaults{}
	coreReq := req.build("model", defaults)

	if coreReq.Model != "model" {
		t.Errorf("expected model 'model', got %q", coreReq.Model)
	}
	if len(coreReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(coreReq.Messages))
	}
	if coreReq.Temperature != 0.8 {
		t.Errorf("expected temperature 0.8, got %f", coreReq.Temperature)
	}
	if coreReq.MaxTokens != 100 {
		t.Errorf("expected maxTokens 100, got %d", coreReq.MaxTokens)
	}
	if coreReq.Metadata["trace"] != "123" {
		t.Errorf("expected metadata trace='123', got %v", coreReq.Metadata["trace"])
	}
}

func TestRequestBuildWithDefaults(t *testing.T) {
	req := Request("provider/model").User("Hello")

	temp := 0.5
	maxTok := 200
	defaults := ClientDefaults{
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	coreReq := req.build("model", defaults)

	if coreReq.Temperature != 0.5 {
		t.Errorf("expected default temperature 0.5, got %f", coreReq.Temperature)
	}
	if coreReq.MaxTokens != 200 {
		t.Errorf("expected default maxTokens 200, got %d", coreReq.MaxTokens)
	}
}

func TestRequestBuildOverridesDefaults(t *testing.T) {
	req := Request("provider/model").
		User("Hello").
		Temperature(0.9).
		MaxTokens(300)

	temp := 0.5
	maxTok := 200
	defaults := ClientDefaults{
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	coreReq := req.build("model", defaults)

	// Request values should override defaults
	if coreReq.Temperature != 0.9 {
		t.Errorf("expected request temperature 0.9, got %f", coreReq.Temperature)
	}
	if coreReq.MaxTokens != 300 {
		t.Errorf("expected request maxTokens 300, got %d", coreReq.MaxTokens)
	}
}

func TestRequestMaxSteps(t *testing.T) {
	req := Request("model").MaxSteps(5)

	if req.stopWhen == nil {
		t.Fatal("expected stopWhen to be set")
	}
}

func TestRequestModel(t *testing.T) {
	req := Request("openai/gpt-4o")

	if req.Model() != "openai/gpt-4o" {
		t.Errorf("expected Model() to return 'openai/gpt-4o', got %q", req.Model())
	}
}

// mockToolHandle implements core.ToolHandle for testing
type mockToolHandle struct {
	name string
}

func (m *mockToolHandle) Name() string {
	return m.name
}

func (m *mockToolHandle) Description() string {
	return "Mock tool"
}

func (m *mockToolHandle) InputSchema() *schema.Schema {
	return &schema.Schema{Type: "object"}
}

func (m *mockToolHandle) OutputSchema() *schema.Schema {
	return nil
}

func (m *mockToolHandle) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	return nil, nil
}

func TestRequestSchema(t *testing.T) {
	type TestOutput struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	req := Request("openai/gpt-4o").
		User("Generate test data").
		Schema(&TestOutput{})

	if req.outputSchema == nil {
		t.Fatal("expected outputSchema to be set")
	}

	// Verify OutputSchema() accessor
	schema := req.OutputSchema()
	if schema == nil {
		t.Fatal("expected OutputSchema() to return non-nil")
	}

	_, ok := schema.(*TestOutput)
	if !ok {
		t.Errorf("expected *TestOutput, got %T", schema)
	}
}

func TestRequestSchemaNotSet(t *testing.T) {
	req := Request("openai/gpt-4o").User("Hello")

	if req.OutputSchema() != nil {
		t.Error("expected OutputSchema() to be nil when not set")
	}
}

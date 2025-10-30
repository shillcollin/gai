package agentx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shillcollin/gai/agentx/state"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/schema"
)

type stubTool struct {
	name     string
	executed bool
}

func (s *stubTool) Name() string                 { return s.name }
func (s *stubTool) Description() string          { return "stub tool" }
func (s *stubTool) InputSchema() *schema.Schema  { return nil }
func (s *stubTool) OutputSchema() *schema.Schema { return nil }
func (s *stubTool) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	s.executed = true
	return map[string]any{"ok": true}, nil
}

type sequenceProvider struct {
	responses []core.TextResult
	idx       int
}

func (s *sequenceProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	res, err := s.pop()
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (s *sequenceProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	res, err := s.pop()
	if err != nil {
		return nil, err
	}
	stream := core.NewStream(ctx, 0)
	go func() {
		defer stream.Close()
		stream.Push(core.StreamEvent{Type: core.EventStart, Schema: "gai.events.v1"})
		stream.Push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: res.Text, Schema: "gai.events.v1"})
		stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", Usage: res.Usage, FinishReason: &core.StopReason{Type: core.StopReasonComplete}})
	}()
	return stream, nil
}

func (s *sequenceProvider) GenerateObject(context.Context, core.Request) (*core.ObjectResultRaw, error) {
	return nil, errors.New("not supported")
}

func (s *sequenceProvider) StreamObject(context.Context, core.Request) (*core.ObjectStreamRaw, error) {
	return nil, errors.New("not supported")
}

func (s *sequenceProvider) Capabilities() core.Capabilities {
	return core.Capabilities{Provider: "stub"}
}

func (s *sequenceProvider) pop() (core.TextResult, error) {
	if s.idx >= len(s.responses) {
		return core.TextResult{}, fmt.Errorf("unexpected call")
	}
	res := s.responses[s.idx]
	s.idx++
	return res, nil
}

func TestNewAgentRequiresIDAndPersona(t *testing.T) {
	_, err := New(AgentOptions{})
	if err == nil {
		t.Fatal("expected error for missing ID")
	}

	_, err = New(AgentOptions{ID: "test"})
	if err == nil {
		t.Fatal("expected error for missing Persona")
	}

	_, err = New(AgentOptions{ID: "test", Persona: "helpful"})
	if err == nil {
		t.Fatal("expected error for missing Provider")
	}
}

func TestNewAgentSetsDefaultLoop(t *testing.T) {
	agent, err := New(AgentOptions{
		ID:       "test",
		Persona:  "helpful",
		Provider: &sequenceProvider{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if agent.opts.Loop == nil {
		t.Fatal("expected default loop to be set")
	}
	if agent.opts.Loop.Name != "default" {
		t.Fatalf("expected default loop, got %s", agent.opts.Loop.Name)
	}
}

func TestDoTaskResumeFromPlan(t *testing.T) {
	dir := t.TempDir()
	progress := filepath.Join(dir, "progress")
	if err := os.MkdirAll(progress, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	findings := filepath.Join(progress, "findings.md")
	if err := os.WriteFile(findings, []byte("- Fact"), 0o644); err != nil {
		t.Fatalf("write findings: %v", err)
	}

	now := time.Now().UTC().UnixMilli()
	existing := state.Record{
		Version:         state.VersionV1,
		LoopName:        "default",
		LoopVersion:     "v1",
		Phase:           "plan",
		CompletedPhases: []string{"gather"},
		Budgets:         state.Budgets{},
		Usage:           state.Usage{},
		StartedAt:       now,
		UpdatedAt:       now,
	}
	if err := state.Write(filepath.Join(progress, "state.json"), existing); err != nil {
		t.Fatalf("write state: %v", err)
	}

	seq := &sequenceProvider{responses: []core.TextResult{
		{Text: `{"version":"plan.v1","goal":"Test","steps":["Run tool"],"allowed_tools":["writer"],"acceptance":["Tool executed"]}`, Usage: core.Usage{TotalTokens: 1}},
		{Text: "Execution complete", Usage: core.Usage{TotalTokens: 1}},
		{Text: "All criteria satisfied", Usage: core.Usage{TotalTokens: 1}},
	}}

	tool := &stubTool{name: "writer"}
	agent, err := New(AgentOptions{
		ID:        "resume-agent",
		Persona:   "You are helpful.",
		Provider:  seq,
		Tools:     []core.ToolHandle{tool},
		Approvals: ApprovalPolicy{},
		DefaultBudgets: Budgets{
			MaxSteps: 5,
		},
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	spec := TaskSpec{Goal: "Test", Budgets: Budgets{MaxSteps: 5}}
	result, err := agent.DoTask(context.Background(), dir, spec)
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	if result.FinishReason.Type != core.StopReasonComplete {
		t.Fatalf("unexpected finish reason: %s", result.FinishReason.Type)
	}
}

func TestDoTaskStopsOnTokenBudget(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "task"), 0o755); err != nil {
		t.Fatalf("mkdir task: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "task", "goal.txt"), []byte("Budget test"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	seq := &sequenceProvider{responses: []core.TextResult{{Text: "Findings", Usage: core.Usage{TotalTokens: 5}}}}

	agent, err := New(AgentOptions{
		ID:        "budget-agent",
		Persona:   "You are concise.",
		Provider:  seq,
		Tools:     []core.ToolHandle{},
		Approvals: ApprovalPolicy{},
		DefaultBudgets: Budgets{
			MaxTokens: 2,
		},
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	result, err := agent.DoTask(context.Background(), root, TaskSpec{Goal: "Budget test", Budgets: Budgets{MaxTokens: 2}})
	if err == nil {
		t.Fatalf("expected budget error")
	}
	if result.FinishReason.Type != "budget.max_tokens" {
		t.Fatalf("expected budget stop reason, got %s", result.FinishReason.Type)
	}
}

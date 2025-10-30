package agentx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/agentx/plan"
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

type memorylessTool struct{ stubTool }

type fakeBroker struct {
	decisions map[string]approvals.Decision
	requests  map[string]approvals.Request
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{
		decisions: make(map[string]approvals.Decision),
		requests:  make(map[string]approvals.Request),
	}
}

func (b *fakeBroker) Submit(_ context.Context, req approvals.Request) (string, error) {
	id := req.ID
	if id == "" {
		id = "req-1"
	}
	req.ID = id
	b.requests[id] = req
	if _, ok := b.decisions[id]; !ok {
		b.decisions[id] = approvals.Decision{ID: id, Status: approvals.StatusApproved, Timestamp: time.Now().UTC()}
	}
	return id, nil
}

func (b *fakeBroker) AwaitDecision(ctx context.Context, id string) (approvals.Decision, error) {
	if dec, ok := b.decisions[id]; ok {
		return dec, nil
	}
	return approvals.Decision{ID: id, Status: approvals.StatusApproved, Timestamp: time.Now().UTC()}, nil
}

func (b *fakeBroker) GetDecision(ctx context.Context, id string) (approvals.Decision, bool, error) {
	dec, ok := b.decisions[id]
	return dec, ok, nil
}

func TestPrepareToolHandlesEnforcesApprovals(t *testing.T) {
	ttool := &stubTool{name: "sandbox_exec"}
	agent := &Agent{toolIndex: map[string]core.ToolHandle{"sandbox_exec": ttool}}

	dir := t.TempDir()
	emitter, err := agentEvents.NewFileEmitter(dir)
	if err != nil {
		t.Fatalf("new emitter: %v", err)
	}

	rc := &runContext{
		agent:       agent,
		ctx:         context.Background(),
		approvals:   newFakeBroker(),
		emitter:     emitter,
		policy:      ApprovalPolicy{RequireTools: []string{"sandbox_exec"}},
		plan:        plan.PlanV1{Steps: []string{"step"}, AllowedTools: []string{"sandbox_exec"}},
		progressDir: dir,
		state:       state.Record{StartedAt: time.Now().UTC().UnixMilli(), UpdatedAt: time.Now().UTC().UnixMilli(), Phase: "act"},
		statePath:   filepath.Join(dir, "state.json"),
	}

	handles, err := rc.prepareToolHandles()
	if err != nil {
		t.Fatalf("prepare handles: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected one tool handle, got %d", len(handles))
	}

	if _, err := handles[0].Execute(context.Background(), map[string]any{"arg": 1}, core.ToolMeta{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !ttool.executed {
		t.Fatalf("tool should have executed")
	}

	broker := rc.approvals.(*fakeBroker)
	if len(broker.requests) == 0 {
		t.Fatalf("expected approval request to be recorded")
	}
}

func TestPrepareToolHandlesSkipsDisallowed(t *testing.T) {
	ttool := &stubTool{name: "allowed"}
	agent := &Agent{toolIndex: map[string]core.ToolHandle{"allowed": ttool, "other": &stubTool{name: "other"}}}

	dir := t.TempDir()
	emitter, _ := agentEvents.NewFileEmitter(dir)

	rc := &runContext{
		agent:       agent,
		ctx:         context.Background(),
		approvals:   newFakeBroker(),
		emitter:     emitter,
		plan:        plan.PlanV1{Steps: []string{"step"}, AllowedTools: []string{"allowed"}},
		progressDir: dir,
		state:       state.Record{StartedAt: time.Now().UTC().UnixMilli(), UpdatedAt: time.Now().UTC().UnixMilli(), Phase: "act"},
		statePath:   filepath.Join(dir, "state.json"),
	}

	handles, err := rc.prepareToolHandles()
	if err != nil {
		t.Fatalf("prepare handles: %v", err)
	}
	if len(handles) != 1 {
		t.Fatalf("expected one handle, got %d", len(handles))
	}
	if handles[0].Name() != "allowed" {
		t.Fatalf("unexpected tool returned: %s", handles[0].Name())
	}
}

type stubMemoryManager struct {
	pinned []core.Message
	phases map[string][]core.Message
}

func (s *stubMemoryManager) BuildPinned(ctx context.Context, taskRoot string) ([]core.Message, error) {
	return s.pinned, nil
}

func (s *stubMemoryManager) BuildPhase(ctx context.Context, phase string, _ any) ([]core.Message, error) {
	return s.phases[strings.ToLower(phase)], nil
}

func TestPhaseMessagesIncludesMemory(t *testing.T) {
	mgr := &stubMemoryManager{
		pinned: []core.Message{core.SystemMessage("Pinned memory")},
		phases: map[string][]core.Message{"gather": {core.UserMessage(core.Text{Text: "Phase memory"})}},
	}

	rc := &runContext{
		agent:  &Agent{opts: AgentOptions{Persona: "You are helpful."}},
		state:  state.Record{Phase: "gather"},
		memory: mgr,
	}

	msgs := rc.phaseMessages("gather", "Instruction")
	if len(msgs) < 3 {
		t.Fatalf("expected pinned, base, and phase messages")
	}
	if msgs[0].Parts[0].(core.Text).Text != "Pinned memory" {
		t.Fatalf("pinned memory not first")
	}
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
		Version:   state.VersionV1,
		Phase:     "plan",
		Budgets:   state.Budgets{},
		Usage:     state.Usage{},
		StartedAt: now,
		UpdatedAt: now,
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
	if seq.idx != len(seq.responses) {
		t.Fatalf("expected provider to be used for remaining phases")
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

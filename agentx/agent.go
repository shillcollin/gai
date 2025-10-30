package agentx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/agentx/internal/id"
	"github.com/shillcollin/gai/agentx/memory"
	"github.com/shillcollin/gai/agentx/state"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
)

// Agent orchestrates AI task execution with pluggable loops, tools, and approval workflows.
type Agent struct {
	opts      AgentOptions
	toolIndex map[string]core.ToolHandle

	// Approvals provides access to the approval broker for external approval handling.
	Approvals approvals.Broker
}

// New constructs an Agent with validated options.
func New(opts AgentOptions) (*Agent, error) {
	if strings.TrimSpace(opts.ID) == "" {
		return nil, errors.New("agent: ID is required")
	}
	if strings.TrimSpace(opts.Persona) == "" {
		return nil, errors.New("agent: persona is required")
	}
	if opts.Provider == nil {
		return nil, errors.New("agent: provider is required")
	}

	// Set default loop if not provided
	if opts.Loop == nil {
		opts.Loop = DefaultLoop
	}

	// Build tool index
	toolIndex := make(map[string]core.ToolHandle, len(opts.Tools))
	for _, h := range opts.Tools {
		name := strings.TrimSpace(h.Name())
		if name == "" {
			return nil, errors.New("agent: tool name cannot be empty")
		}
		if _, exists := toolIndex[name]; exists {
			return nil, fmt.Errorf("agent: duplicate tool name %q", name)
		}
		toolIndex[name] = h
	}

	return &Agent{
		opts:      opts,
		toolIndex: toolIndex,
		Approvals: nil, // Will be set per-task in DoTask
	}, nil
}

// DoTask executes a task using the configured loop.
func (a *Agent) DoTask(ctx context.Context, root string, spec TaskSpec, opts ...DoTaskOption) (DoTaskResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Parse options
	cfg := doTaskConfig{view: a.opts.DefaultResultView}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.view == 0 {
		cfg.view = ResultViewMinimal
	}

	// Prepare directories
	taskRoot := filepath.Clean(root)
	progressDir := filepath.Join(taskRoot, "progress")
	if err := os.MkdirAll(progressDir, 0o755); err != nil {
		return DoTaskResult{}, fmt.Errorf("agent: create progress dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "task"), 0o755); err != nil {
		return DoTaskResult{}, fmt.Errorf("agent: create task dir: %w", err)
	}

	// Set up emitters
	emitter, err := a.setupEmitter(progressDir)
	if err != nil {
		return DoTaskResult{}, err
	}

	// Set up approval broker
	broker, err := a.setupApprovalBroker(progressDir)
	if err != nil {
		return DoTaskResult{}, err
	}
	a.Approvals = broker

	// Set up runner
	taskRunner := a.setupRunner()

	// Resolve budgets
	budgets := a.mergeBudgets(spec.Budgets)

	// Apply wall clock timeout
	taskCtx := ctx
	var cancel context.CancelFunc
	if budgets.MaxWallClock > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, budgets.MaxWallClock)
		defer cancel()
	}

	// Load or initialize state
	startTs := agentEvents.Now()
	statePath := filepath.Join(progressDir, "state.json")
	initialState, err := a.loadOrInitState(statePath, budgets, startTs)
	if err != nil {
		return DoTaskResult{}, err
	}

	// Check if already finished
	if initialState.Phase == "finished" {
		return DoTaskResult{}, fmt.Errorf("agent: task already finished")
	}

	// Generate task ID
	taskID, err := id.New()
	if err != nil {
		return DoTaskResult{}, err
	}

	// Set loop info in state
	initialState.LoopName = a.opts.Loop.Name
	initialState.LoopVersion = a.opts.Loop.Version
	if err := state.Write(statePath, initialState); err != nil {
		return DoTaskResult{}, err
	}

	// Build loop context
	lc := &LoopContext{
		Ctx:          taskCtx,
		loop:         a.opts.Loop,
		agent:        a,
		task:         TaskEnv{root: taskRoot},
		state:        &initialState,
		statePath:    statePath,
		spec:         spec,
		budgets:      budgets,
		emitter:      emitter,
		approvals:    broker,
		runnerInst:   taskRunner,
		memoryMgr:    resolveMemoryManager(a.opts.Memory, taskRoot, spec, a.opts.ID),
		warnings:     make([]core.Warning, 0),
		steps:        make([]StepReport, 0),
		finishReason: core.StopReason{},
	}

	// Execute loop
	execErr := a.opts.Loop.Execute(lc)

	// Mark as finished
	lc.state.Phase = "finished"
	lc.state.UpdatedAt = agentEvents.Now()
	_ = state.Write(statePath, *lc.state)

	// Emit finish event
	if lc.finishReason.Type == "" {
		if execErr != nil {
			lc.finishReason = core.StopReason{Type: core.StopReasonUnknown, Description: execErr.Error()}
		} else {
			lc.finishReason = core.StopReason{Type: core.StopReasonComplete}
		}
	}

	_ = lc.Emit(agentEvents.AgentEventV1{
		ID:   lc.newEventID(),
		Type: agentEvents.TypeFinish,
		Step: "finish",
		Data: map[string]any{"reason": lc.finishReason.Type},
	})

	// Build result
	return a.buildResult(lc, taskID, cfg, execErr), execErr
}

func (a *Agent) setupEmitter(progressDir string) (Emitter, error) {
	// Always include file emitter
	fileEmitter, err := agentEvents.NewFileEmitter(progressDir)
	if err != nil {
		return nil, err
	}

	if len(a.opts.Emitters) == 0 {
		return fileEmitter, nil
	}

	// Combine file emitter with configured emitters
	emitters := append([]Emitter{fileEmitter}, a.opts.Emitters...)
	return NewMultiEmitter(emitters...), nil
}

func (a *Agent) setupApprovalBroker(progressDir string) (approvals.Broker, error) {
	if a.opts.ApprovalBroker != nil {
		return a.opts.ApprovalBroker, nil
	}

	// Default to hybrid broker for durability + speed
	return approvals.NewHybridBroker(progressDir)
}

func (a *Agent) setupRunner() *runner.Runner {
	runnerOpts := append([]runner.RunnerOption(nil), a.opts.RunnerOptions...)

	// Add skill assets if configured
	if len(a.opts.Skills) > 0 && a.opts.SandboxManager != nil {
		skill := a.opts.Skills[0]
		if skill.Skill != nil {
			runnerOpts = append(runnerOpts, runner.WithSkillAssets(
				skill.Skill,
				a.opts.SandboxManager,
				mergeAssets(skill.Assets, a.opts.SandboxAssets),
			))
		}
	}

	return runner.New(a.opts.Provider, runnerOpts...)
}

func (a *Agent) loadOrInitState(statePath string, budgets Budgets, startTs int64) (state.Record, error) {
	if existing, err := state.Read(statePath); err == nil {
		return existing, nil
	} else if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "state: read") {
		initialState := state.Record{
			Version:         state.VersionV1,
			Phase:           "initialising",
			CompletedPhases: []string{},
			Budgets:         toStateBudgets(budgets),
			Usage:           state.Usage{},
			StartedAt:       startTs,
			UpdatedAt:       startTs,
		}
		if err := state.Write(statePath, initialState); err != nil {
			return state.Record{}, err
		}
		return initialState, nil
	} else {
		return state.Record{}, err
	}
}

func (a *Agent) resolveApprovalPolicy(spec TaskSpec) ApprovalPolicy {
	policy := a.opts.Approvals
	if spec.RequirePlanApproval {
		policy.RequirePlan = true
	}
	if spec.Approvals.RequirePlan {
		policy.RequirePlan = true
	}
	if len(spec.Approvals.RequireTools) > 0 {
		policy.RequireTools = append([]string(nil), spec.Approvals.RequireTools...)
	}
	return policy
}

func (a *Agent) mergeBudgets(task Budgets) Budgets {
	merged := a.opts.DefaultBudgets
	if task.MaxWallClock > 0 {
		merged.MaxWallClock = task.MaxWallClock
	}
	if task.MaxSteps > 0 {
		merged.MaxSteps = task.MaxSteps
	}
	if task.MaxConsecutiveToolSteps > 0 {
		merged.MaxConsecutiveToolSteps = task.MaxConsecutiveToolSteps
	}
	if task.MaxTokens > 0 {
		merged.MaxTokens = task.MaxTokens
	}
	if task.MaxCostUSD > 0 {
		merged.MaxCostUSD = task.MaxCostUSD
	}
	return merged
}

func (a *Agent) buildResult(lc *LoopContext, taskID string, cfg doTaskConfig, execErr error) DoTaskResult {
	result := DoTaskResult{
		AgentID:     a.opts.ID,
		TaskID:      taskID,
		LoopName:    a.opts.Loop.Name,
		LoopVersion: a.opts.Loop.Version,
		ProgressDir: lc.task.Progress("."),
		OutputText:  lc.outputText,
		Usage: core.Usage{
			InputTokens:  lc.state.Usage.InputTokens,
			OutputTokens: lc.state.Usage.OutputTokens,
			TotalTokens:  lc.state.Usage.TotalTokens,
			CostUSD:      lc.state.Usage.CostUSD,
		},
		FinishReason: lc.finishReason,
		Warnings:     append([]core.Warning(nil), lc.warnings...),
		StartedAt:    lc.state.StartedAt,
		CompletedAt:  agentEvents.Now(),
	}

	if cfg.view >= ResultViewFull {
		result.Steps = append([]StepReport(nil), lc.steps...)
	}

	return result
}

func (a *Agent) newEventID() (string, error) {
	return id.New()
}

func toStateBudgets(b Budgets) state.Budgets {
	return state.Budgets{
		MaxWallClockMS:          int64(b.MaxWallClock / time.Millisecond),
		MaxSteps:                b.MaxSteps,
		MaxConsecutiveToolSteps: b.MaxConsecutiveToolSteps,
		MaxTokens:               b.MaxTokens,
		MaxCostUSD:              b.MaxCostUSD,
	}
}

func resolveMemoryManager(cfg MemoryManager, taskRoot string, spec TaskSpec, agentID string) MemoryManager {
	if cfg != nil {
		return cfg
	}
	scope := memory.Scope{Kind: "agent", ID: agentID}
	if spec.UserID != "" {
		scope = memory.Scope{Kind: "user", ID: spec.UserID}
	}
	mgr := memory.NewManager(memory.Strategy{}, memory.NullProvider{}, scope)
	return &memoryAdapter{mgr: mgr}
}

type memoryAdapter struct {
	mgr *memory.Manager
}

func (m *memoryAdapter) BuildPinned(ctx context.Context, _ string) ([]core.Message, error) {
	return m.mgr.BuildPinned(ctx)
}

func (m *memoryAdapter) BuildPhase(ctx context.Context, phase string, _ any) ([]core.Message, error) {
	return m.mgr.BuildPhase(ctx, phase)
}

func mergeAssets(primary, fallback sandbox.SessionAssets) sandbox.SessionAssets {
	merged := fallback
	if primary.Workspace != "" {
		merged.Workspace = primary.Workspace
	}
	if len(primary.Mounts) > 0 {
		merged.Mounts = primary.Mounts
	}
	return merged
}

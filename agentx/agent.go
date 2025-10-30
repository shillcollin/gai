package agentx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/agentx/internal/id"
	"github.com/shillcollin/gai/agentx/memory"
	"github.com/shillcollin/gai/agentx/plan"
	"github.com/shillcollin/gai/agentx/state"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
)

// Agent coordinates Gather → Plan → Act → Verify for a task.
type Agent struct {
	opts      AgentOptions
	toolIndex map[string]core.ToolHandle
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
	if opts.ApprovalBrokerFactory == nil {
		opts.ApprovalBrokerFactory = DefaultApprovalBrokerFactory
	}

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

	return &Agent{opts: opts, toolIndex: toolIndex}, nil
}

// DefaultApprovalBrokerFactory constructs a file-based approval broker under the task progress directory.
func DefaultApprovalBrokerFactory(progressDir string) (approvals.Broker, error) {
	return approvals.NewFileBroker(progressDir)
}

// DoTask executes the default loop for a task rooted at root.
func (a *Agent) DoTask(ctx context.Context, root string, spec TaskSpec, opts ...DoTaskOption) (DoTaskResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := doTaskConfig{view: a.opts.DefaultResultView}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.view == 0 {
		cfg.view = ResultViewMinimal
	}

	taskRoot := filepath.Clean(root)
	progressDir := filepath.Join(taskRoot, "progress")
	if err := os.MkdirAll(progressDir, 0o755); err != nil {
		return DoTaskResult{}, fmt.Errorf("agent: create progress dir: %w", err)
	}

	emitter, err := agentEvents.NewFileEmitter(progressDir)
	if err != nil {
		return DoTaskResult{}, err
	}

	broker, err := a.opts.ApprovalBrokerFactory(progressDir)
	if err != nil {
		return DoTaskResult{}, err
	}

	runnerOpts := append([]runner.RunnerOption(nil), a.opts.RunnerOptions...)
	if len(a.opts.Skills) > 0 && a.opts.SandboxManager != nil {
		skill := a.opts.Skills[0]
		if skill.Skill != nil {
			runnerOpts = append(runnerOpts, runner.WithSkillAssets(skill.Skill, a.opts.SandboxManager, mergeAssets(skill.Assets, a.opts.SandboxAssets)))
		}
	}
	taskRunner := runner.New(a.opts.Provider, runnerOpts...)

	policy := a.resolveApprovalPolicy(spec)
	budgets := a.mergeBudgets(spec.Budgets)

	taskCtx := ctx
	var cancel context.CancelFunc
	if budgets.MaxWallClock > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, budgets.MaxWallClock)
		defer cancel()
	}

	startTs := agentEvents.Now()
	statePath := filepath.Join(progressDir, "state.json")
	initialState := state.Record{}
	if existing, err := state.Read(statePath); err == nil {
		if existing.Phase == "finished" {
			return DoTaskResult{}, fmt.Errorf("agent: task already finished")
		}
		initialState = existing
		startTs = existing.StartedAt
	} else if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "state: read") {
		initialState = state.Record{
			Version:   state.VersionV1,
			Phase:     "initialising",
			Budgets:   toStateBudgets(budgets),
			Usage:     state.Usage{},
			StartedAt: startTs,
			UpdatedAt: startTs,
		}
		if err := state.Write(statePath, initialState); err != nil {
			return DoTaskResult{}, err
		}
	} else if err != nil {
		return DoTaskResult{}, err
	}

	taskID, err := id.New()
	if err != nil {
		return DoTaskResult{}, err
	}

	run := &runContext{
		agent:       a,
		ctx:         taskCtx,
		provider:    a.opts.Provider,
		runner:      taskRunner,
		approvals:   broker,
		emitter:     emitter,
		policy:      policy,
		budgets:     budgets,
		spec:        spec,
		taskRoot:    taskRoot,
		progressDir: progressDir,
		state:       initialState,
		statePath:   statePath,
		taskID:      taskID,
		memory:      resolveMemoryManager(a.opts.Memory, taskRoot, spec, a.opts.ID),
	}

	if err := run.execute(); err != nil {
		return run.finalise(cfg, err)
	}
	return run.finalise(cfg, nil)
}

// resolveApprovalPolicy merges agent defaults with per-task overrides.
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

// mergeBudgets overlays per-task budgets onto defaults.
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

// runContext carries mutable state for a single task execution.
type runContext struct {
	agent        *Agent
	ctx          context.Context
	provider     core.Provider
	runner       *runner.Runner
	approvals    approvals.Broker
	emitter      *agentEvents.FileEmitter
	policy       ApprovalPolicy
	budgets      Budgets
	spec         TaskSpec
	taskRoot     string
	progressDir  string
	state        state.Record
	statePath    string
	plan         plan.PlanV1
	steps        []StepReport
	deliverables []DeliverableRef
	warnings     []core.Warning
	outputText   string
	finishReason core.StopReason
	taskID       string
	memory       MemoryManager
}

func (r *runContext) execute() error {
	// TODO(loop): expose loop composition via AgentOptions so callers can supply
	// custom phase graphs instead of the fixed Gather→Plan→Act→Verify sequence.
	// The helpers below are already reusable; we just need a LoopFunc seam.
	phases := []struct {
		name string
		fn   func(*runContext) error
	}{
		{"gather", phaseGather},
		{"plan", phasePlan},
		{"act", phaseAct},
		{"verify", phaseVerify},
	}

	start := phaseIndex(r.state.Phase)
	if start >= len(phases) {
		return nil
	}

	if len(r.plan.Steps) == 0 && r.state.PlanSig != "" {
		if err := r.loadExistingPlan(); err != nil {
			return err
		}
	}

	for i := start; i < len(phases); i++ {
		if err := r.phase(phases[i].name, phases[i].fn); err != nil {
			return err
		}
	}

	r.state.Phase = "finished"
	r.state.UpdatedAt = agentEvents.Now()
	if err := state.Write(r.statePath, r.state); err != nil {
		return err
	}
	return r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeFinish,
		Step: "finish",
		Data: map[string]any{"reason": core.StopReasonComplete},
	})
}

func (r *runContext) phase(name string, fn func(*runContext) error) error {
	if err := r.advancePhase(name); err != nil {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{ID: r.newEventID(), Type: agentEvents.TypePhaseStart, Step: name}); err != nil {
		return err
	}
	start := time.Now()
	if err := fn(r); err != nil {
		if r.finishReason.Type == "" {
			r.finishReason = core.StopReason{Type: core.StopReasonUnknown, Description: err.Error()}
		}
		_ = r.emit(agentEvents.AgentEventV1{ID: r.newEventID(), Type: agentEvents.TypeError, Step: name, Error: err.Error()})
		return err
	}
	duration := time.Since(start).Milliseconds()
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypePhaseFinish,
		Step: name,
		Data: map[string]any{"duration_ms": duration},
	}); err != nil {
		return err
	}
	return nil
}

func (r *runContext) advancePhase(phase string) error {
	r.state.Phase = phase
	r.state.UpdatedAt = agentEvents.Now()
	return state.Write(r.statePath, r.state)
}

// phaseGather assembles context from task inputs and produces findings.
func phaseGather(r *runContext) error {
	summary, err := r.collectTaskContext()
	if err != nil {
		return err
	}

	messages := r.phaseMessages("gather", "Gather relevant information and record key findings as concise markdown bullet points.")
	messages = append(messages, core.UserMessage(core.TextPart(fmt.Sprintf(
		"Task goal: %s\n\nTask inputs:\n%s\n\nProduce findings.",
		r.spec.Goal, summary,
	))))

	res, err := r.provider.GenerateText(r.ctx, core.Request{
		Messages:    messages,
		MaxTokens:   800,
		Temperature: 0.2,
	})
	if err != nil {
		return fmt.Errorf("agent: gather generation failed: %w", err)
	}
	if err := r.recordUsage(res.Usage); err != nil {
		return err
	}
	r.warnings = append(r.warnings, res.Warnings...)

	findingsPath := filepath.Join(r.progressDir, "findings.md")
	if err := writeAtomic(findingsPath, []byte(strings.TrimSpace(res.Text)+"\n")); err != nil {
		return err
	}
	if err := r.appendNote("Gather phase completed"); err != nil {
		return err
	}
	r.recordStep(StepReport{
		ID:        "gather",
		Name:      "gather",
		Status:    "completed",
		OutputRef: relPath(r.progressDir, findingsPath),
		Usage:     res.Usage,
	})
	r.outputText = res.Text
	return nil
}

// phasePlan prepares or validates the executable plan.
func phasePlan(r *runContext) error {
	planDir := filepath.Join(r.progressDir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return fmt.Errorf("agent: create plan dir: %w", err)
	}
	planPath := filepath.Join(planDir, "current.json")

	var p plan.PlanV1
	data, err := os.ReadFile(planPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			p, err = r.generatePlan()
			if err != nil {
				return err
			}
			bytes, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				return fmt.Errorf("agent: encode plan: %w", err)
			}
			if err := writeAtomic(planPath, bytes); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("agent: read plan: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("agent: decode plan: %w", err)
		}
		if err := p.Validate(); err != nil {
			return err
		}
	}

	if len(p.AllowedTools) == 0 {
		p.AllowedTools = r.availableToolNames()
	}
	if err := p.Validate(); err != nil {
		return err
	}
	sig, err := p.Signature()
	if err != nil {
		return err
	}
	r.plan = p
	r.state.PlanSig = sig
	r.state.UpdatedAt = agentEvents.Now()
	if err := state.Write(r.statePath, r.state); err != nil {
		return err
	}

	if r.policy.RequirePlan {
		if err := r.requestPlanApproval(sig); err != nil {
			return err
		}
	}

	r.recordStep(StepReport{
		ID:        "plan",
		Name:      "plan",
		Status:    "completed",
		OutputRef: relPath(r.progressDir, planPath),
	})
	return nil
}

// phaseAct executes the plan using the runner and allowed tools.
func phaseAct(r *runContext) error {
	if len(r.plan.Steps) == 0 {
		return errors.New("agent: plan contains no steps")
	}

	toolHandles, err := r.prepareToolHandles()
	if err != nil {
		return err
	}

	planSummary := strings.Join(r.plan.Steps, "\n- ")
	acceptance := strings.Join(r.plan.Acceptance, "\n- ")

	messages := r.phaseMessages("act", "Execute the plan step-by-step, using tools when beneficial. Follow the plan strictly and provide a final response summarising results.")
	messages = append(messages,
		core.AssistantMessage(fmt.Sprintf("Plan:\n- %s", planSummary)),
		core.AssistantMessage(fmt.Sprintf("Acceptance criteria:\n- %s", acceptance)),
		core.UserMessage(core.TextPart(fmt.Sprintf("Task goal: %s", r.spec.Goal))),
	)

	req := core.Request{
		Messages:    messages,
		Tools:       toolHandles,
		ToolChoice:  core.ToolChoiceAuto,
		Temperature: 0.2,
	}

	var stops []core.StopCondition
	if r.budgets.MaxSteps > 0 {
		stops = append(stops, core.MaxSteps(r.budgets.MaxSteps))
	}
	if r.budgets.MaxConsecutiveToolSteps > 0 {
		stops = append(stops, stopWhenMaxConsecutiveToolSteps(r.budgets.MaxConsecutiveToolSteps))
	}
	stops = append(stops, core.NoMoreTools())
	req.StopWhen = core.Any(stops...)

	res, err := r.runner.ExecuteRequest(r.ctx, req)
	if err != nil {
		return fmt.Errorf("agent: act execution failed: %w", err)
	}
	if err := r.recordUsage(res.Usage); err != nil {
		return err
	}
	r.warnings = append(r.warnings, res.Warnings...)

	stepReports := convertRunnerSteps(res.Steps)
	r.steps = append(r.steps, stepReports...)

	outputPath := filepath.Join(r.progressDir, "steps", "act", "output.md")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("agent: create step dir: %w", err)
	}
	if err := writeAtomic(outputPath, []byte(res.Text)); err != nil {
		return err
	}

	r.deliverables = append(r.deliverables, DeliverableRef{
		Path: relPath(r.progressDir, outputPath),
		Kind: "report",
		MIME: "text/markdown",
	})
	r.outputText = res.Text
	return nil
}

// phaseVerify validates acceptance criteria and records verification output.
func phaseVerify(r *runContext) error {
	messages := r.phaseMessages("verify", "Verify whether the acceptance criteria have been met. Respond with a structured markdown report.")
	messages = append(messages,
		core.AssistantMessage(fmt.Sprintf("Acceptance criteria:\n- %s", strings.Join(r.plan.Acceptance, "\n- "))),
		core.UserMessage(core.TextPart(fmt.Sprintf("Final output to verify:\n%s", r.outputText))),
	)

	res, err := r.provider.GenerateText(r.ctx, core.Request{
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   600,
	})
	if err != nil {
		return fmt.Errorf("agent: verify generation failed: %w", err)
	}
	if err := r.recordUsage(res.Usage); err != nil {
		return err
	}
	r.warnings = append(r.warnings, res.Warnings...)

	verifyDir := filepath.Join(r.progressDir, "verify")
	if err := os.MkdirAll(verifyDir, 0o755); err != nil {
		return fmt.Errorf("agent: create verify dir: %w", err)
	}
	reportPath := filepath.Join(verifyDir, "0001-report.md")
	if err := writeAtomic(reportPath, []byte(res.Text)); err != nil {
		return err
	}

	r.recordStep(StepReport{
		ID:        "verify",
		Name:      "verify",
		Status:    "completed",
		OutputRef: relPath(r.progressDir, reportPath),
		Usage:     res.Usage,
	})
	return nil
}

func (r *runContext) generatePlan() (plan.PlanV1, error) {
	findings, _ := os.ReadFile(filepath.Join(r.progressDir, "findings.md"))
	toolNames := r.availableToolNames()
	sort.Strings(toolNames)
	prompt := fmt.Sprintf(`You are planning the steps for an AI worker.
Goal: %s
Findings:
%s

Return a strict JSON object with fields:
- goal (string)
- steps (array of strings)
- allowed_tools (array of tool names selected from [%s])
- acceptance (array of strings)
`, r.spec.Goal, string(findings), strings.Join(toolNames, ", "))

	messages := r.phaseMessages("plan", "Generate a structured plan in JSON using the specified schema.")
	messages = append(messages, core.UserMessage(core.TextPart(prompt)))

	res, err := r.provider.GenerateText(r.ctx, core.Request{
		Messages:    messages,
		Temperature: 0,
		MaxTokens:   800,
	})
	if err != nil {
		return plan.PlanV1{}, fmt.Errorf("agent: plan generation failed: %w", err)
	}
	if err := r.recordUsage(res.Usage); err != nil {
		return plan.PlanV1{}, err
	}
	r.warnings = append(r.warnings, res.Warnings...)

	responsePath := filepath.Join(r.progressDir, "plan", "latest_response.txt")
	if err := writeAtomic(responsePath, []byte(res.Text)); err != nil {
		return plan.PlanV1{}, fmt.Errorf("agent: write plan response: %w", err)
	}

	raw := extractJSON(res.Text)
	rawPath := filepath.Join(r.progressDir, "plan", "latest_raw.json")
	if err := writeAtomic(rawPath, []byte(strings.TrimSpace(raw)+"\n")); err != nil {
		return plan.PlanV1{}, fmt.Errorf("agent: write plan raw: %w", err)
	}
	var p plan.PlanV1
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return plan.PlanV1{}, fmt.Errorf("agent: parse plan JSON: %w", err)
	}
	if len(p.AllowedTools) == 0 {
		p.AllowedTools = toolNames
	}
	if err := p.Validate(); err != nil {
		return plan.PlanV1{}, err
	}
	return p, nil
}

func (r *runContext) requestPlanApproval(planSig string) error {
	req := approvals.Request{
		Kind:      approvals.KindPlan,
		PlanPath:  "plan/current.json",
		PlanSig:   planSig,
		Rationale: fmt.Sprintf("Plan approval for goal: %s", r.spec.Goal),
		ExpiresAt: time.Now().UTC().Add(2 * time.Hour),
	}
	id, err := r.approvals.Submit(r.ctx, req)
	if err != nil {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeApprovalRequested,
		Step: "plan",
		Data: map[string]any{"approval_id": id, "kind": approvals.KindPlan},
	}); err != nil {
		return err
	}
	decision, err := r.approvals.AwaitDecision(r.ctx, id)
	if err != nil && !errors.Is(err, approvals.ErrExpired) {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeApprovalDecided,
		Step: "plan",
		Data: map[string]any{"approval_id": id, "status": decision.Status},
	}); err != nil {
		return err
	}
	switch decision.Status {
	case approvals.StatusApproved:
		return nil
	case approvals.StatusDenied:
		return errors.New("agent: plan approval denied")
	case approvals.StatusExpired:
		return errors.New("agent: plan approval expired")
	default:
		return nil
	}
}

func (r *runContext) prepareToolHandles() ([]core.ToolHandle, error) {
	allowed := make(map[string]struct{})
	for _, name := range r.plan.AllowedTools {
		allowed[name] = struct{}{}
	}
	requireSet := make(map[string]struct{})
	for _, name := range r.policy.RequireTools {
		requireSet[name] = struct{}{}
	}

	var handles []core.ToolHandle
	for name, handle := range r.agent.toolIndex {
		if _, ok := allowed[name]; !ok {
			continue
		}
		handles = append(handles, &approvalToolHandle{
			ToolHandle:     handle,
			run:            r,
			requireSet:     requireSet,
			planAllowedSet: allowed,
		})
	}
	sort.Slice(handles, func(i, j int) bool {
		return handles[i].Name() < handles[j].Name()
	})
	if len(handles) == 0 {
		// Provide plan instructions even if no tools will run.
		return nil, nil
	}
	return handles, nil
}

// approvalToolHandle wraps a tool to enforce plan allowances and approvals.
type approvalToolHandle struct {
	core.ToolHandle
	run            *runContext
	requireSet     map[string]struct{}
	planAllowedSet map[string]struct{}
}

func (h *approvalToolHandle) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	if _, ok := h.planAllowedSet[h.Name()]; !ok {
		return nil, fmt.Errorf("agent: tool %s not permitted by plan", h.Name())
	}
	if _, ok := h.requireSet[h.Name()]; ok {
		if err := h.run.requestToolApproval(ctx, h.Name(), input); err != nil {
			return nil, err
		}
	}
	return h.ToolHandle.Execute(ctx, input, meta)
}

func (r *runContext) requestToolApproval(ctx context.Context, toolName string, input map[string]any) error {
	req := approvals.Request{
		Kind:      approvals.KindTool,
		ToolName:  toolName,
		Rationale: fmt.Sprintf("Tool %s invocation", toolName),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		Metadata:  map[string]any{"input": input},
	}
	id, err := r.approvals.Submit(ctx, req)
	if err != nil {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeApprovalRequested,
		Step: "act",
		Data: map[string]any{"approval_id": id, "kind": approvals.KindTool, "tool": toolName},
	}); err != nil {
		return err
	}
	decision, err := r.approvals.AwaitDecision(ctx, id)
	if err != nil && !errors.Is(err, approvals.ErrExpired) {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeApprovalDecided,
		Step: "act",
		Data: map[string]any{"approval_id": id, "status": decision.Status, "tool": toolName},
	}); err != nil {
		return err
	}
	switch decision.Status {
	case approvals.StatusApproved:
		return nil
	case approvals.StatusDenied:
		return fmt.Errorf("agent: tool %s approval denied", toolName)
	case approvals.StatusExpired:
		return fmt.Errorf("agent: tool %s approval expired", toolName)
	default:
		return nil
	}
}

func (r *runContext) availableToolNames() []string {
	names := make([]string, 0, len(r.agent.toolIndex))
	for name := range r.agent.toolIndex {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *runContext) composeBaseMessages(instruction string) []core.Message {
	persona := strings.TrimSpace(r.agent.opts.Persona)
	if persona == "" {
		persona = "You are a helpful AI agent."
	}
	systemText := fmt.Sprintf("%s\nYou are working on phase: %s", persona, r.state.Phase)
	messages := []core.Message{core.SystemMessage(systemText)}
	if instruction != "" {
		messages = append(messages, core.AssistantMessage(instruction))
	}
	return messages
}

func (r *runContext) collectTaskContext() (string, error) {
	taskDir := filepath.Join(r.taskRoot, "task")
	var entries []string
	err := filepath.WalkDir(taskDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := relPath(taskDir, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > 32*1024 {
			data = data[:32*1024]
		}
		entries = append(entries, fmt.Sprintf("# %s\n%s", rel, string(data)))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("agent: walk task inputs: %w", err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n\n"), nil
}

func (r *runContext) appendNote(entry string) error {
	notesPath := filepath.Join(r.progressDir, "notes.md")
	line := fmt.Sprintf("- [%s] %s\n", time.Now().UTC().Format(time.RFC3339), entry)
	f, err := os.OpenFile(notesPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("agent: open notes: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return fmt.Errorf("agent: write notes: %w", err)
	}
	return f.Close()
}

func (r *runContext) recordUsage(delta core.Usage) error {
	if delta == (core.Usage{}) {
		return nil
	}
	r.state.Usage.InputTokens += delta.InputTokens
	r.state.Usage.OutputTokens += delta.OutputTokens
	r.state.Usage.TotalTokens += delta.TotalTokens
	r.state.Usage.CostUSD += delta.CostUSD
	r.state.UpdatedAt = agentEvents.Now()
	if err := state.Write(r.statePath, r.state); err != nil {
		return err
	}
	if err := r.emit(agentEvents.AgentEventV1{
		ID:   r.newEventID(),
		Type: agentEvents.TypeUsageDelta,
		Data: map[string]any{
			"input_tokens":      delta.InputTokens,
			"output_tokens":     delta.OutputTokens,
			"total_tokens":      delta.TotalTokens,
			"cost_usd":          delta.CostUSD,
			"budgets_remaining": r.budgetsRemaining(),
		},
	}); err != nil {
		return err
	}

	if r.budgets.MaxTokens > 0 && r.state.Usage.TotalTokens > r.budgets.MaxTokens {
		r.finishReason = core.StopReason{Type: "budget.max_tokens"}
		return fmt.Errorf("agent: token budget exceeded")
	}
	if r.budgets.MaxCostUSD > 0 && r.state.Usage.CostUSD > r.budgets.MaxCostUSD {
		r.finishReason = core.StopReason{Type: "budget.max_cost_usd"}
		return fmt.Errorf("agent: cost budget exceeded")
	}
	return nil
}

func (r *runContext) emit(event agentEvents.AgentEventV1) error {
	if event.Ts == 0 {
		event.Ts = agentEvents.Now()
	}
	if event.Version == "" {
		event.Version = agentEvents.SchemaVersionV1
	}
	return r.emitter.Emit(event)
}

func (r *runContext) newEventID() string {
	if idv, err := id.New(); err == nil {
		return idv
	}
	return fmt.Sprintf("ev_%d", time.Now().UnixNano())
}

func (r *runContext) recordStep(step StepReport) {
	r.steps = append(r.steps, step)
}

func (r *runContext) finalise(cfg doTaskConfig, execErr error) (DoTaskResult, error) {
	completed := agentEvents.Now()
	result := DoTaskResult{
		AgentID:      r.agent.opts.ID,
		TaskID:       r.taskID,
		LoopName:     "default",
		LoopVersion:  "v1",
		ProgressDir:  r.progressDir,
		Deliverables: append([]DeliverableRef(nil), r.deliverables...),
		OutputText:   r.outputText,
		Usage: core.Usage{
			InputTokens:  r.state.Usage.InputTokens,
			OutputTokens: r.state.Usage.OutputTokens,
			TotalTokens:  r.state.Usage.TotalTokens,
			CostUSD:      r.state.Usage.CostUSD,
		},
		FinishReason: r.finishReason,
		Warnings:     append([]core.Warning(nil), r.warnings...),
		StartedAt:    r.state.StartedAt,
		CompletedAt:  completed,
		Ext:          cfg.ext,
	}

	if result.FinishReason.Type == "" {
		if execErr != nil {
			result.FinishReason = core.StopReason{Type: core.StopReasonUnknown, Description: execErr.Error()}
		} else {
			result.FinishReason = core.StopReason{Type: core.StopReasonComplete}
		}
	}

	if cfg.view >= ResultViewFull {
		result.Steps = append([]StepReport(nil), r.steps...)
	}

	if execErr != nil {
		return result, execErr
	}
	return result, nil
}

// stopWhenMaxConsecutiveToolSteps mirrors the pattern documented in docs/AGENT.md.
func stopWhenMaxConsecutiveToolSteps(limit int) core.StopCondition {
	if limit <= 0 {
		return func(state *core.RunnerState) (bool, core.StopReason) {
			return false, core.StopReason{}
		}
	}
	return func(state *core.RunnerState) (bool, core.StopReason) {
		if state == nil || len(state.Steps) == 0 {
			return false, core.StopReason{}
		}
		consecutive := 0
		for i := len(state.Steps) - 1; i >= 0; i-- {
			if len(state.Steps[i].ToolCalls) == 0 {
				break
			}
			consecutive++
		}
		if consecutive >= limit {
			return true, core.StopReason{Type: "budget.max_consecutive_tool_steps"}
		}
		return false, core.StopReason{}
	}
}

func (r *runContext) phaseMessages(phase string, instruction string) []core.Message {
	base := r.composeBaseMessages(instruction)
	pinned := r.memoryMessages(r.ctx, "pinned")
	phaseMem := r.memoryMessages(r.ctx, phase)
	messages := make([]core.Message, 0, len(pinned)+len(base)+len(phaseMem))
	messages = append(messages, pinned...)
	messages = append(messages, base...)
	messages = append(messages, phaseMem...)
	return messages
}

func (r *runContext) memoryMessages(ctx context.Context, phase string) []core.Message {
	if r.memory == nil {
		return nil
	}
	var (
		msgs []core.Message
		err  error
	)
	switch phase {
	case "pinned":
		msgs, err = r.memory.BuildPinned(ctx, r.taskRoot)
	default:
		msgs, err = r.memory.BuildPhase(ctx, phase, nil)
	}
	if err != nil {
		_ = r.emit(agentEvents.AgentEventV1{
			ID:    r.newEventID(),
			Type:  agentEvents.TypeError,
			Step:  phase,
			Error: fmt.Sprintf("memory retrieval error: %v", err),
		})
		return nil
	}
	return append([]core.Message(nil), msgs...)
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

func (r *runContext) budgetsRemaining() map[string]any {
	remaining := map[string]any{}
	if r.budgets.MaxTokens > 0 {
		rem := r.budgets.MaxTokens - r.state.Usage.TotalTokens
		if rem < 0 {
			rem = 0
		}
		remaining["tokens"] = rem
	}
	if r.budgets.MaxCostUSD > 0 {
		rem := r.budgets.MaxCostUSD - r.state.Usage.CostUSD
		if rem < 0 {
			rem = 0
		}
		remaining["cost_usd"] = rem
	}
	if r.budgets.MaxSteps > 0 {
		rem := r.budgets.MaxSteps - len(r.steps)
		if rem < 0 {
			rem = 0
		}
		remaining["steps"] = rem
	}
	return remaining
}

func phaseIndex(current string) int {
	switch strings.ToLower(current) {
	case "", "initialising", "gather":
		return 0
	case "plan":
		return 1
	case "act":
		return 2
	case "verify":
		return 3
	case "finished":
		return 4
	default:
		return 0
	}
}

func (r *runContext) loadExistingPlan() error {
	planBytes, err := os.ReadFile(filepath.Join(r.progressDir, "plan", "current.json"))
	if err != nil {
		return fmt.Errorf("agent: load plan: %w", err)
	}
	var p plan.PlanV1
	if err := json.Unmarshal(planBytes, &p); err != nil {
		return fmt.Errorf("agent: decode plan: %w", err)
	}
	if err := p.Validate(); err != nil {
		return err
	}
	r.plan = p
	return nil
}

func convertRunnerSteps(steps []core.Step) []StepReport {
	reports := make([]StepReport, 0, len(steps))
	for _, s := range steps {
		report := StepReport{
			ID:        fmt.Sprintf("step-%d", s.Number),
			Name:      fmt.Sprintf("runner-step-%d", s.Number),
			Status:    "completed",
			StartedAt: s.StartedAt,
			EndedAt:   s.CompletedAt,
			ToolsUsed: extractToolNames(s.ToolCalls),
			ToolCalls: append([]core.ToolExecution(nil), s.ToolCalls...),
			Usage:     s.Usage,
		}
		reports = append(reports, report)
	}
	return reports
}

func extractToolNames(calls []core.ToolExecution) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Call.Name)
	}
	sort.Strings(names)
	return names
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
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

func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		// Remove code fences
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

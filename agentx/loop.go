package agentx

import (
	"context"
	"fmt"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	agentEvents "github.com/shillcollin/gai/agentx/events"
	"github.com/shillcollin/gai/agentx/state"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
)

// Loop defines an agent's execution pattern.
type Loop struct {
	// Name identifies this loop (e.g., "default", "simple-act", "iterative")
	Name string

	// Version for compatibility tracking (e.g., "v1")
	Version string

	// Phases declares expected phases for progress calculation.
	// Optional: if omitted, UIs show simple progress view.
	// Weights should sum to 1.0 for accurate percentage calculation.
	Phases []PhaseInfo

	// Execute is the actual loop logic.
	Execute LoopFunc
}

// PhaseInfo describes a phase for progress tracking and UI display.
type PhaseInfo struct {
	Name        string  // Phase identifier (e.g., "gather", "plan")
	Description string  // Human-readable description for UIs
	Weight      float64 // Relative weight for progress calculation (0.0-1.0)
}

// LoopFunc is the signature for loop execution functions.
type LoopFunc func(ctx *LoopContext) error

// LoopContext provides structured access to agent capabilities and state management.
type LoopContext struct {
	Ctx  context.Context
	loop *Loop

	agent       *Agent
	task        TaskEnv
	state       *state.Record
	statePath   string
	spec        TaskSpec
	budgets     Budgets
	emitter     Emitter
	approvals   approvals.Broker
	runnerInst  *runner.Runner
	memoryMgr   MemoryManager
	warnings    []core.Warning
	outputText  string
	steps       []StepReport
	finishReason core.StopReason
}

// Phase executes a named phase with automatic event emission and resumability.
// This is the high-level helper for standard phase execution.
func (lc *LoopContext) Phase(name string, fn func() error) error {
	// Check if phase already completed (resumability)
	if lc.IsPhaseComplete(name) {
		if err := lc.Emit(agentEvents.AgentEventV1{
			ID:      lc.newEventID(),
			Type:    agentEvents.TypePhaseSkipped,
			Step:    name,
			Message: "Phase already completed (resuming from checkpoint)",
		}); err != nil {
			return err
		}
		return nil
	}

	// Emit phase start
	start := time.Now()
	if err := lc.EmitPhaseStart(name); err != nil {
		return err
	}

	// Execute user function
	fnErr := fn()
	duration := time.Since(start)

	// Handle errors
	if fnErr != nil {
		_ = lc.Emit(agentEvents.AgentEventV1{
			ID:    lc.newEventID(),
			Type:  agentEvents.TypeError,
			Step:  name,
			Error: fnErr.Error(),
		})
		return fnErr
	}

	// Emit phase finish
	if err := lc.EmitPhaseFinish(name, duration); err != nil {
		return err
	}

	// Mark complete for resumability
	return lc.MarkPhaseComplete(name)
}

// EmitPhaseStart emits a phase.start event with progress metadata.
func (lc *LoopContext) EmitPhaseStart(name string) error {
	// Update state
	lc.state.Phase = name
	lc.state.UpdatedAt = agentEvents.Now()
	if err := state.Write(lc.statePath, *lc.state); err != nil {
		return err
	}

	// Calculate progress
	progress := lc.calculateProgress(name, phaseStarting)

	// Emit event
	return lc.Emit(agentEvents.AgentEventV1{
		ID:       lc.newEventID(),
		Type:     agentEvents.TypePhaseStart,
		Step:     name,
		Progress: progress,
	})
}

// EmitPhaseFinish emits a phase.finish event with duration and progress.
func (lc *LoopContext) EmitPhaseFinish(name string, duration time.Duration) error {
	progress := lc.calculateProgress(name, phaseFinishing)

	return lc.Emit(agentEvents.AgentEventV1{
		ID:   lc.newEventID(),
		Type: agentEvents.TypePhaseFinish,
		Step: name,
		Data: map[string]any{
			"duration_ms": duration.Milliseconds(),
		},
		Progress: progress,
	})
}

// Emit sends an event through the configured emitter.
func (lc *LoopContext) Emit(event agentEvents.AgentEventV1) error {
	if event.Ts == 0 {
		event.Ts = agentEvents.Now()
	}
	if event.Version == "" {
		event.Version = agentEvents.SchemaVersionV1
	}
	return lc.emitter.Emit(event)
}

// IsPhaseComplete checks if a phase has already been completed.
func (lc *LoopContext) IsPhaseComplete(name string) bool {
	for _, completed := range lc.state.CompletedPhases {
		if completed == name {
			return true
		}
	}
	return false
}

// MarkPhaseComplete records a phase as completed in state.
func (lc *LoopContext) MarkPhaseComplete(name string) error {
	if lc.IsPhaseComplete(name) {
		return nil
	}
	lc.state.CompletedPhases = append(lc.state.CompletedPhases, name)
	lc.state.UpdatedAt = agentEvents.Now()
	return state.Write(lc.statePath, *lc.state)
}

// GetCompletedPhases returns the list of completed phase names.
func (lc *LoopContext) GetCompletedPhases() []string {
	return append([]string(nil), lc.state.CompletedPhases...)
}

// RequestApproval submits an approval request and blocks until decided.
func (lc *LoopContext) RequestApproval(req approvals.Request) error {
	id, err := lc.approvals.Submit(lc.Ctx, req)
	if err != nil {
		return err
	}

	if err := lc.Emit(agentEvents.AgentEventV1{
		ID:   lc.newEventID(),
		Type: agentEvents.TypeApprovalRequested,
		Step: lc.state.Phase,
		Data: map[string]any{
			"approval_id": id,
			"kind":        req.Kind,
		},
	}); err != nil {
		return err
	}

	decision, err := lc.approvals.AwaitDecision(lc.Ctx, id)
	if err != nil {
		return err
	}

	if err := lc.Emit(agentEvents.AgentEventV1{
		ID:   lc.newEventID(),
		Type: agentEvents.TypeApprovalDecided,
		Step: lc.state.Phase,
		Data: map[string]any{
			"approval_id": id,
			"status":      decision.Status,
		},
	}); err != nil {
		return err
	}

	if decision.Status != approvals.StatusApproved {
		return fmt.Errorf("approval denied or expired")
	}

	return nil
}

// Provider returns the AI provider.
func (lc *LoopContext) Provider() core.Provider {
	return lc.agent.opts.Provider
}

// Runner returns the configured runner.
func (lc *LoopContext) Runner() *runner.Runner {
	return lc.runnerInst
}

// Tools returns the available tool handles.
func (lc *LoopContext) Tools() []core.ToolHandle {
	tools := make([]core.ToolHandle, 0, len(lc.agent.toolIndex))
	for _, tool := range lc.agent.toolIndex {
		tools = append(tools, tool)
	}
	return tools
}

// Memory returns the memory manager.
func (lc *LoopContext) Memory() MemoryManager {
	return lc.memoryMgr
}

// Task returns the task environment for file access.
func (lc *LoopContext) Task() TaskEnv {
	return lc.task
}

// State returns the current state record.
func (lc *LoopContext) State() *state.Record {
	return lc.state
}

// Spec returns the task specification.
func (lc *LoopContext) Spec() TaskSpec {
	return lc.spec
}

// RecordUsage updates usage tracking and checks budgets.
func (lc *LoopContext) RecordUsage(delta core.Usage) error {
	if delta == (core.Usage{}) {
		return nil
	}

	lc.state.Usage.InputTokens += delta.InputTokens
	lc.state.Usage.OutputTokens += delta.OutputTokens
	lc.state.Usage.TotalTokens += delta.TotalTokens
	lc.state.Usage.CostUSD += delta.CostUSD
	lc.state.UpdatedAt = agentEvents.Now()

	if err := state.Write(lc.statePath, *lc.state); err != nil {
		return err
	}

	if err := lc.Emit(agentEvents.AgentEventV1{
		ID:   lc.newEventID(),
		Type: agentEvents.TypeUsageDelta,
		Data: map[string]any{
			"input_tokens":      delta.InputTokens,
			"output_tokens":     delta.OutputTokens,
			"total_tokens":      delta.TotalTokens,
			"cost_usd":          delta.CostUSD,
			"budgets_remaining": lc.budgetsRemaining(),
		},
	}); err != nil {
		return err
	}

	// Check budget limits
	if lc.budgets.MaxTokens > 0 && lc.state.Usage.TotalTokens > lc.budgets.MaxTokens {
		lc.finishReason = core.StopReason{Type: "budget.max_tokens"}
		return fmt.Errorf("token budget exceeded")
	}
	if lc.budgets.MaxCostUSD > 0 && lc.state.Usage.CostUSD > lc.budgets.MaxCostUSD {
		lc.finishReason = core.StopReason{Type: "budget.max_cost_usd"}
		return fmt.Errorf("cost budget exceeded")
	}

	return nil
}

// RecordStep adds a step report to the execution history.
func (lc *LoopContext) RecordStep(step StepReport) {
	lc.steps = append(lc.steps, step)
}

// AddWarning records warning messages.
func (lc *LoopContext) AddWarning(warnings ...core.Warning) {
	lc.warnings = append(lc.warnings, warnings...)
}

// SetOutputText sets the final output text.
func (lc *LoopContext) SetOutputText(text string) {
	lc.outputText = text
}

// SetFinishReason sets the task completion reason.
func (lc *LoopContext) SetFinishReason(reason core.StopReason) {
	lc.finishReason = reason
}

type progressMoment int

const (
	phaseStarting progressMoment = iota
	phaseFinishing
)

// calculateProgress computes progress metadata based on loop structure.
func (lc *LoopContext) calculateProgress(currentPhase string, moment progressMoment) *agentEvents.ProgressInfo {
	info := &agentEvents.ProgressInfo{
		LoopName:         lc.loop.Name,
		LoopVersion:      lc.loop.Version,
		CurrentPhase:     currentPhase,
		CompletedPhases:  lc.GetCompletedPhases(),
		BudgetsRemaining: lc.budgetsRemaining(),
	}

	// If loop declared phases, compute rich progress
	if len(lc.loop.Phases) > 0 {
		info.TotalPhases = len(lc.loop.Phases)

		// Find current phase
		for i, p := range lc.loop.Phases {
			if p.Name == currentPhase {
				info.CurrentPhaseIndex = i
				info.CurrentPhaseDescription = p.Description
				break
			}
		}

		// Calculate percent complete based on weights
		var completed float64

		// Add weight for completed phases
		for _, p := range lc.loop.Phases {
			if contains(info.CompletedPhases, p.Name) {
				completed += p.Weight
			}
		}

		// Add partial weight for current phase if finishing
		if moment == phaseFinishing {
			for _, p := range lc.loop.Phases {
				if p.Name == currentPhase {
					completed += p.Weight
					break
				}
			}
		}

		info.PercentComplete = completed * 100
	}

	return info
}

func (lc *LoopContext) budgetsRemaining() map[string]any {
	remaining := make(map[string]any)

	if lc.budgets.MaxTokens > 0 {
		rem := lc.budgets.MaxTokens - lc.state.Usage.TotalTokens
		if rem < 0 {
			rem = 0
		}
		remaining["tokens"] = rem
	}

	if lc.budgets.MaxCostUSD > 0 {
		rem := lc.budgets.MaxCostUSD - lc.state.Usage.CostUSD
		if rem < 0 {
			rem = 0
		}
		remaining["cost_usd"] = rem
	}

	if lc.budgets.MaxSteps > 0 {
		rem := lc.budgets.MaxSteps - len(lc.steps)
		if rem < 0 {
			rem = 0
		}
		remaining["steps"] = rem
	}

	return remaining
}

func (lc *LoopContext) newEventID() string {
	id, err := lc.agent.newEventID()
	if err != nil {
		return fmt.Sprintf("ev_%d", time.Now().UnixNano())
	}
	return id
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

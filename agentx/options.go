package agentx

import (
	"context"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/skills"
)

// Budgets captures hard and soft limits for a task. Zero values disable a budget.
type Budgets struct {
	MaxWallClock            time.Duration
	MaxSteps                int
	MaxConsecutiveToolSteps int
	MaxTokens               int
	MaxCostUSD              float64
}

// ApprovalPolicy defines which checkpoints require human approval.
type ApprovalPolicy struct {
	RequirePlan  bool
	RequireTools []string
}

// AgentOptions configure an Agent instance.
type AgentOptions struct {
	ID            string
	Persona       string
	Provider      core.Provider
	Tools         []core.ToolHandle
	Skills        []SkillBinding
	Memory        MemoryManager
	Approvals     ApprovalPolicy
	Observability obs.Options
	SeedMessages  []core.Message

	DefaultResultView ResultView
	DefaultBudgets    Budgets

	RunnerOptions         []runner.RunnerOption
	SandboxManager        *sandbox.Manager
	SandboxAssets         sandbox.SessionAssets
	ApprovalBrokerFactory ApprovalBrokerFactory
}

// TaskSpec defines a single task execution request.
type TaskSpec struct {
	Goal         string
	UserID       string
	SeedMessages []core.Message

	Budgets   Budgets
	Approvals ApprovalPolicy

	RequirePlanApproval bool
}

// ResultView controls how much data is returned from DoTask.
type ResultView int

const (
	ResultViewMinimal ResultView = iota
	ResultViewSummary
	ResultViewFull
)

// DoTaskOption applies optional behaviour to DoTask.
type DoTaskOption func(*doTaskConfig)

type doTaskConfig struct {
	view ResultView
	ext  map[string]any
}

// WithResultView returns a DoTask option that selects the desired view.
func WithResultView(view ResultView) DoTaskOption {
	return func(cfg *doTaskConfig) {
		cfg.view = view
	}
}

// WithExt attaches arbitrary metadata to the DoTaskResult.
func WithExt(ext map[string]any) DoTaskOption {
	return func(cfg *doTaskConfig) {
		cfg.ext = ext
	}
}

// MemoryManager provides pre-rendered memory snippets for prompts.
type MemoryManager interface {
	BuildPinned(ctx context.Context, taskRoot string) ([]core.Message, error)
	BuildPhase(ctx context.Context, phase string, taskCtx any) ([]core.Message, error)
}

// SkillBinding ties a skill manifest to an identifier. Implemented in later milestones.
type SkillBinding struct {
	Name   string
	Skill  *skills.Skill
	Assets sandbox.SessionAssets
}

// ApprovalBrokerFactory constructs a broker for a task-specific progress directory.
type ApprovalBrokerFactory func(progressDir string) (approvals.Broker, error)

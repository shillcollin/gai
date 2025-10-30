# GAI Agent Example — End‑to‑End Walkthrough

This document shows a plausible end‑state developer experience for building and running a GAI Agent using the design in docs/AGENT.md. It includes:

- Directory layout with `task/` inputs and `progress/` working area
- Multi‑channel memory (Agent + User) with pinned and phase‑specific injection
- Approvals model (file‑based) for plan and tool calls
- Default loop (Gather → Plan → Act → Verify) with per‑step tools and stop conditions
- Placeholder tools for repo search, test running, and JSON validation
- Observability/journaling patterns to understand runs

Note: This example uses conceptual types for the evolving Agent API (agentx). It demonstrates how the final integration should feel, and maps directly to existing gai primitives (`core`, `runner`, `tools`, `sandbox`, `obs`).

## The Agent Object And DoTask()

We expose a single high-level API: `Agent.DoTask(ctx, root, spec, opts...)` which prepares the task directories, composes memory and context, runs the default loop (Gather → Plan → Act → Verify), and finalizes/journals results. For flexibility, the Agent also supports `DoTaskCollect[T]` and `DoTaskAsync` for custom projections and streaming orchestration.

Conceptual Agent shape:

```go
package agentx // conceptual package name for example

import (
  "context"
  "time"
  "github.com/shillcollin/gai/core"
  "github.com/shillcollin/gai/runner"
)

type AgentOptions struct {
  ID        string
  Persona   string
  Provider  core.Provider
  Tools     []core.ToolHandle
  Memory    MemoryProvider
  Approvals ApprovalPolicy // v1: RequirePlan bool, RequireTools []string
  SeedMessages []core.Message
}

// v1 budgets: simple, enforceable limits
type Budgets struct {
  MaxWallClock time.Duration
  MaxSteps int
  MaxConsecutiveToolSteps int
  StepToolTimeout time.Duration
}

type TaskSpec struct {
  Goal       string
  UserID     string
  RequirePlanApproval bool
  SeedMessages []core.Message
  Budgets     Budgets
}

type DoTaskResult struct {
  AgentID     string
  TaskID      string
  ProgressDir string
  OutputText  string
  Steps       []StepReport // conceptual; mirrors docs/AGENT.md
}

type Agent struct {
  id       string
  persona  string
  provider core.Provider
  runner   *runner.Runner
  taskRunner *runner.Runner // per-task runner honoring budgets (timeouts)
  tools    []core.ToolHandle

  mem   MemoryManager
  appr  ApprovalBroker // file-based broker in v1

  opts    AgentOptions
  history []core.Message // seeded and accrued conversation snippets
  runBuds Budgets        // budgets for the current task run
}

func New(opts AgentOptions, r *runner.Runner, mm MemoryManager, ab ApprovalBroker) *Agent {
  return &Agent{ id: opts.ID, persona: opts.Persona, provider: opts.Provider, runner: r, tools: opts.Tools, mem: mm, appr: ab, opts: opts }
}

// DoTask: orchestrates the default loop (Gather → Plan → Act → Verify)
func (a *Agent) DoTask(ctx context.Context, root string, spec TaskSpec) (DoTaskResult, error) {
  env, err := prepareTaskEnv(root) // ensures task/ and progress/
  if err != nil { return DoTaskResult{}, err }

  // Apply basic budgets
  a.runBuds = spec.Budgets
  if a.runBuds.MaxWallClock > 0 {
    var cancel context.CancelFunc
    ctx, cancel = context.WithTimeout(ctx, a.runBuds.MaxWallClock)
    defer cancel()
  }

  // Build a per-task runner to honor StepToolTimeout; fall back to the agent's default runner
  if a.runBuds.StepToolTimeout > 0 {
    a.taskRunner = runner.New(a.provider,
      runner.WithToolTimeout(a.runBuds.StepToolTimeout),
      runner.WithOnToolError(runner.ToolErrorAppendAndContinue),
    )
  } else {
    a.taskRunner = a.runner
  }

  if spec.UserID != "" {
    _ = a.mem.Attach(MemoryBinding{ Scope: MemoryScope{Kind: "user", ID: spec.UserID, AgentID: a.id}, Strategy: DefaultUserStrategy(), Enabled: true })
  }
  _ = a.mem.Attach(MemoryBinding{ Scope: MemoryScope{Kind: "agent", ID: a.id, AgentID: a.id}, Strategy: DefaultAgentStrategy(), Enabled: true })

  // Seed message history (global + task-scoped). In real code, also support seed from prior session IDs or task/history.jsonl
  if len(a.opts.SeedMessages) > 0 { a.SeedHistory(a.opts.SeedMessages...) }
  if len(spec.SeedMessages) > 0 { a.SeedHistory(spec.SeedMessages...) }

  if err := a.stepGather(ctx, env); err != nil { return DoTaskResult{}, err }
  if err := a.stepPlan(ctx, env, spec.RequirePlanApproval); err != nil { return DoTaskResult{}, err }
  if err := a.stepAct(ctx, env); err != nil { return DoTaskResult{}, err }
  if err := a.stepVerify(ctx, env); err != nil { return DoTaskResult{}, err }

  // In a real agent, finalize: write state.json, memory writes, obs logs
  return DoTaskResult{ TaskID: "task-xyz", AgentID: a.id, ProgressDir: env.Progress("."), OutputText: "See verify report" }, nil
}

// Helpers used by steps (conceptual)
type TaskEnv struct{ root string }
func (e TaskEnv) Task(p string) string    { return pathJoin(e.root, "task", p) }
func (e TaskEnv) Progress(p string) string{ return pathJoin(e.root, "progress", p) }
func prepareTaskEnv(root string) (TaskEnv, error) { /* mkdir -p task/ progress/; seed state.json */ }

// History controls (conceptual)
type HistoryPolicy struct { Mode string; N int; MaxTokens int; IncludeTools bool }

func (a *Agent) SeedHistory(msgs ...core.Message) { a.history = append(a.history, msgs...) }

func (a *Agent) HistorySnapshot(p HistoryPolicy) []core.Message {
  switch strings.ToLower(p.Mode) {
  case "none", "":
    return nil
  case "seedonly":
    return append([]core.Message(nil), a.history...)
  case "lastn":
    n := p.N
    if n <= 0 || n >= len(a.history) { return append([]core.Message(nil), a.history...) }
    return append([]core.Message(nil), a.history[len(a.history)-n:]...)
  case "summary":
    // In real code, summarize with the provider into a compact system message
    sum := core.SystemMessage("History summary: prior conversation available; truncated for brevity.")
    return []core.Message{sum}
  default:
    return nil
  }
}

// Stop condition helper for consecutive tool-call steps (conceptual)
func stopWhenMaxConsecutiveToolSteps(limit int) core.StopCondition {
  if limit <= 0 {
    return func(state *core.RunnerState) (bool, core.StopReason) { return false, core.StopReason{} }
  }
  return func(state *core.RunnerState) (bool, core.StopReason) {
    if state == nil || len(state.Steps) == 0 { return false, core.StopReason{} }
    consecutive := 0
    for i := len(state.Steps) - 1; i >= 0; i-- {
      if len(state.Steps[i].ToolCalls) == 0 { break }
      consecutive++
    }
    if consecutive >= limit {
      return true, core.StopReason{ Type: core.StopReasonMaxSteps, Description: fmt.Sprintf("reached %d consecutive tool-call steps", limit) }
    }
    return false, core.StopReason{}
  }
}
```

The step functions (`stepGather`, `stepPlan`, `stepAct`, `stepVerify`) are regular Go methods that compose prompts and call the provider/runner. This allows developers to replace the default loop with a custom `DoTask` or to inject a `LoopFunc` that the agent will execute.

## Directory Layout

```
my-task/
  task/
    bug.md                 # The bug report (inputs only; agent never writes here)
  progress/                # Agent working area (notes, plan, artifacts, approvals)
    notes.md
    todo.md
    state.json
```

Example `task/bug.md` content:

```
Title: Off‑by‑one in pagination

Context:
  - The /api/items endpoint accepts page (1-based) and page_size.
  - When page=1 and page_size=10, item 11 sometimes appears on page 1.

Hypothesis:
  - The server likely uses zero-based offset incorrectly.

Acceptance criteria:
  - Write a failing test that reproduces the issue.
  - Fix the bug, then ensure the test passes.
  - Verify pagination boundaries across pages 1..3 with page_size=10.
```

## Tools — Placeholders With Realistic Shapes

We define three typed tools using `tools.New[TIn, TOut]`. In a real deployment, these would call actual services or run inside a sandbox. For this example, they simulate behavior.

```go
package main

import (
  "context"
  "fmt"
  "strings"
  "time"

  "github.com/shillcollin/gai/tools"
)

// 1) repo_search — search a repository for files/symbols likely related to the bug
type RepoSearchInput struct {
  Query     string   `json:"query"`
  Globs     []string `json:"globs,omitempty"`
  MaxFiles  int      `json:"max_files,omitempty" default:"20"`
}
type RepoSearchOutput struct {
  Files []string `json:"files"`
  Hints []string `json:"hints"`
}
var repoSearch = tools.New[RepoSearchInput, RepoSearchOutput](
  "repo_search",
  "Search codebase for relevant files",
  func(ctx context.Context, in RepoSearchInput, meta tools.Meta) (RepoSearchOutput, error) {
    // Placeholder: a real tool would walk/grep/index; we simulate a hit
    files := []string{"internal/pagination/pager.go", "internal/pagination/pager_test.go"}
    hints := []string{"Check off-by-one in offset calculation", "Verify 1-based page to 0-based index"}
    if in.MaxFiles > 0 && len(files) > in.MaxFiles {
      files = files[:in.MaxFiles]
    }
    return RepoSearchOutput{Files: files, Hints: hints}, nil
  },
)

// 2) run_tests — run unit tests (simulated). In reality, use sandbox_exec with `go test ./...` and parse output
type RunTestsInput struct {
  Packages []string `json:"packages,omitempty"`
  Filter   string   `json:"filter,omitempty"`
  TimeoutS int      `json:"timeout_s,omitempty" default:"60"`
}
type RunTestsOutput struct {
  Passed      bool     `json:"passed"`
  FailedTests []string `json:"failed_tests,omitempty"`
  Output      string   `json:"output"`
}
var runTests = tools.New[RunTestsInput, RunTestsOutput](
  "run_tests",
  "Run unit tests and return status",
  func(ctx context.Context, in RunTestsInput, meta tools.Meta) (RunTestsOutput, error) {
    // Placeholder: pretend we ran tests and one failed until we "fix" it
    if strings.Contains(in.Filter, "pagination") {
      return RunTestsOutput{Passed: false, FailedTests: []string{"TestPagination_Page1"}, Output: "--- FAIL: TestPagination_Page1"}, nil
    }
    return RunTestsOutput{Passed: true, Output: "ok"}, nil
  },
)

// 3) json_validate — ensure structured outputs match expectations
type JSONValidateInput struct {
  JSON string `json:"json"`
  Schema string `json:"schema"`
}
type JSONValidateOutput struct {
  Valid  bool   `json:"valid"`
  Errors string `json:"errors,omitempty"`
}
var jsonValidate = tools.New[JSONValidateInput, JSONValidateOutput](
  "json_validate",
  "Validate JSON against a schema",
  func(ctx context.Context, in JSONValidateInput, meta tools.Meta) (JSONValidateOutput, error) {
    // Placeholder always valid
    time.Sleep(50 * time.Millisecond)
    return JSONValidateOutput{Valid: true}, nil
  },
)
```

When passed to the runner, wrap typed tools with `tools.NewCoreAdapter(...)` to get `core.ToolHandle`s, or use tool helpers like `tools.NewSandboxCommand(...)` that already return a core handle.

## Memory — Multi‑Channel (Agent + User)

We attach two channels: the Agent’s own and the active User’s. We define small pinned blocks and phase‑specific recalls.

```go
package main

import (
  "time"
  agentx "github.com/shillcollin/gai/agentx"
)

func defaultAgentStrategy() agentx.InjectionStrategy {
  s := agentx.InjectionStrategy{}
  s.Pinned.Enabled = true
  s.Pinned.MaxTokens = 80
  s.Phases = map[string]agentx.PhaseInjection{
    "gather": { Queries: []agentx.MemoryQuery{{Name: "agent_facts", TopK: 4, MaxTokens: 200}} },
    "plan":   { Queries: []agentx.MemoryQuery{{Name: "agent_facts", TopK: 3, MaxTokens: 150}} },
    "verify": { Queries: []agentx.MemoryQuery{{Name: "agent_facts", TopK: 2, MaxTokens: 120}} },
  }
  s.Dedupe.ByHash = true
  return s
}

func defaultUserStrategy() agentx.InjectionStrategy {
  s := agentx.InjectionStrategy{}
  s.Pinned.Enabled = true
  s.Pinned.MaxTokens = 60
  s.Phases = map[string]agentx.PhaseInjection{
    "gather": { Queries: []agentx.MemoryQuery{{Name: "user_prefs", TopK: 3, MaxTokens: 120}} },
    "plan":   { Queries: []agentx.MemoryQuery{{Name: "user_prefs", TopK: 3, MaxTokens: 120}} },
    "verify": { Queries: []agentx.MemoryQuery{{Name: "user_prefs", TopK: 1, MaxTokens: 80}} },
  }
  s.Dedupe.ByTitle = true
  return s
}
```

## Approvals — v1 File‑Based Broker

We keep v1 approvals simple: a boolean to require plan approval and a static list of tool names that require approval before each call. Requests go under `progress/approvals/requests/`; decisions under `progress/approvals/decisions/`.

```go
package main

import (
  "context"
  "encoding/json"
  "errors"
  "os"
  "path/filepath"
  "time"
)

type ApprovalRequest struct {
  ID        string    `json:"id"`             // unique request id
  Type      string    `json:"type"`           // "plan"|"tool"
  PlanPath  string    `json:"plan_path,omitempty"`
  ToolName  string    `json:"tool_name,omitempty"`
  Rationale string    `json:"rationale,omitempty"`
  ExpiresAt time.Time `json:"expires_at"`
}
type ApprovalDecision struct {
  ID         string    `json:"id"`
  Decision   string    `json:"decision"` // approved|denied|expired
  ApprovedBy string    `json:"approved_by"`
  Timestamp  time.Time `json:"timestamp"`
}

func writeApprovalRequest(progressDir string, req ApprovalRequest) error {
  path := filepath.Join(progressDir, "approvals", "requests")
  if err := os.MkdirAll(path, 0o755); err != nil { return err }
  f := filepath.Join(path, req.ID+".json")
  data, _ := json.MarshalIndent(req, "", "  ")
  return os.WriteFile(f, data, 0o644)
}

func waitForDecision(ctx context.Context, progressDir, id string) (ApprovalDecision, error) {
  path := filepath.Join(progressDir, "approvals", "decisions", id+".json")
  ticker := time.NewTicker(500 * time.Millisecond)
  defer ticker.Stop()
  for {
    select {
    case <-ctx.Done():
      return ApprovalDecision{}, ctx.Err()
    case <-ticker.C:
      data, err := os.ReadFile(path)
      if err == nil {
        var d ApprovalDecision
        if json.Unmarshal(data, &d) == nil {
          return d, nil
        }
      }
    }
  }
}
```

For local demos, you can also provide an “auto‑approve” path that writes the decision immediately; in production, wire Slack/HTTP on top of the same on‑disk schema.

## Loop — Gather → Plan → Act → Verify (inside Agent)

Below is a consolidated example assembling prompts, using per‑step tools, and respecting approvals. It uses runner for iterative steps and single calls for one‑shot steps.

```go
package agentx

import (
  "context"
  "crypto/sha1"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "time"

  "github.com/google/uuid"
  "github.com/shillcollin/gai/core"
  "github.com/shillcollin/gai/tools"
)

type StepName string
const (
  StepGather StepName = "gather"
  StepPlan   StepName = "plan"
  StepAct    StepName = "act"
  StepVerify StepName = "verify"
)

type LoopBudgets struct {
  MaxCostUSD   float64
  MaxWallClock time.Duration
}

// assembleMessages builds a deterministic prompt with persona, pinned memory, task context, phase recalls, and user request
func (a *Agent) assembleMessages(persona string, pinned []core.Message, ctxFiles []string, phaseRecalls []core.Message, userReq string) []core.Message {
  msgs := []core.Message{ core.SystemMessage(strings.TrimSpace(persona)) }
  msgs = append(msgs, pinned...)
  if len(ctxFiles) > 0 {
    b := &strings.Builder{}
    fmt.Fprintln(b, "Task Context:")
    for _, p := range ctxFiles { fmt.Fprintf(b, "\n# %s\n", p) }
    msgs = append(msgs, core.SystemMessage(b.String()))
  }
  msgs = append(msgs, phaseRecalls...)
  if s := strings.TrimSpace(userReq); s != "" {
    msgs = append(msgs, core.UserMessage(core.TextPart(s)))
  }
  return msgs
}

// readBug loads the bug description from task/bug.md
func readBug(taskDir string) (string, error) {
  data, err := os.ReadFile(filepath.Join(taskDir, "bug.md"))
  if err != nil { return "", err }
  return string(data), nil
}

// cheap content hash for approvals
func hash(data string) string { h := sha1.Sum([]byte(data)); return hex.EncodeToString(h[:]) }

// runGather: explore task and repo, write findings to progress/findings.md
func (a *Agent) stepGather(ctx context.Context, env TaskEnv) error {
  // Build memory messages
  pinned, _ := a.mem.BuildPinned(ctx)
  phase, _ := a.mem.BuildPhase(ctx, "gather", nil)
  // Render seed history according to per-step policy (example: use LastN(4) here)
  history := a.HistorySnapshot(HistoryPolicy{Mode: "LastN", N: 4, IncludeTools: false})
  msgs := append(a.assembleMessages(a.persona, pinned, []string{"task/bug.md"}, phase, "Extract key facts and hypothesize root causes."), history...)
  // Allow only read‑oriented tools
  max := a.runBuds.MaxSteps
  if max <= 0 { max = 4 }
  req := core.Request{
    Messages: msgs,
    Tools: []core.ToolHandle{
      tools.NewCoreAdapter(repoSearch),
    },
    ToolChoice: core.ToolChoiceAuto,
    StopWhen: core.Any(
      core.MaxSteps(max),
      stopWhenMaxConsecutiveToolSteps(a.runBuds.MaxConsecutiveToolSteps),
      core.NoMoreTools(),
    ),
  }
  result, err := a.taskRunner.ExecuteRequest(ctx, req)
  if err != nil { return err }
  _ = os.MkdirAll(env.Progress("."), 0o755)
  _ = os.WriteFile(env.Progress("findings.md"), []byte(strings.TrimSpace(result.Text)), 0o644)
  return nil
}

// runPlan: produce a structured plan JSON and (optionally) gate on approval
type Plan struct {
  Goal   string   `json:"goal"`
  Steps  []string `json:"steps"`
  Tools  []string `json:"tools"`
}

func (a *Agent) stepPlan(ctx context.Context, env TaskEnv, requireApproval bool) error {
  pinned, _ := a.mem.BuildPinned(ctx)
  phase, _ := a.mem.BuildPhase(ctx, "plan", nil)
  bug, err := readBug(env.Task("."))
  if err != nil { return err }
  findings, _ := os.ReadFile(env.Progress("findings.md"))
  history := a.HistorySnapshot(HistoryPolicy{Mode: "SeedOnly"})
  msgs := append(a.assembleMessages(a.persona, pinned, []string{"progress/findings.md"}, phase, fmt.Sprintf("Based on the bug and findings, produce a minimal plan to write a failing test, fix code, and re-run tests.\n\nBug:\n%s\n\nFindings:\n%s", bug, string(findings))), history...)
  // One‑shot structured output (simplified):
  if _, err := a.provider.GenerateText(ctx, core.Request{Messages: msgs, MaxTokens: 400}); err != nil { return err }
  plan := Plan{Goal: "Fix pagination bug", Steps: []string{"Write failing test", "Implement fix", "Re-run tests"}, Tools: []string{"run_tests", "repo_search"}}
  _ = os.MkdirAll(env.Progress("plan"), 0o755)
  _ = os.WriteFile(env.Progress("plan/current.json"), []byte(fmt.Sprintf("{\n  \"goal\": %q,\n  \"steps\": [\n    %q,\n    %q,\n    %q\n  ],\n  \"tools\": [\n    %q, %q\n  ]\n}\n", plan.Goal, plan.Steps[0], plan.Steps[1], plan.Steps[2], plan.Tools[0], plan.Tools[1])), 0o644)

  if requireApproval {
    id := uuid.NewString()
    req := ApprovalRequest{ID: id, Type: "plan", PlanPath: env.Progress("plan/current.json"), Rationale: "Plan to fix pagination bug", ExpiresAt: time.Now().Add(2 * time.Hour)}
    if err := writeApprovalRequest(env.Progress("."), req); err != nil { return err }
    // In demos, auto‑approve immediately (comment out to require human approval)
    _ = func() error {
      dec := ApprovalDecision{ID: id, Decision: "approved", ApprovedBy: "demo", Timestamp: time.Now()}
      p := filepath.Join(env.Progress("approvals"), "decisions")
      _ = os.MkdirAll(p, 0o755)
      data, _ := json.Marshal(dec)
      return os.WriteFile(filepath.Join(p, id+".json"), data, 0o644)
    }()
    if _, err := waitForDecision(ctx, env.Progress("."), id); err != nil { return err }
  }
  return nil
}

// runAct: perform steps with tool access; respect tool approvals for risky tools
func (a *Agent) stepAct(ctx context.Context, env TaskEnv) error {
  pinned, _ := a.mem.BuildPinned(ctx)
  phase, _ := a.mem.BuildPhase(ctx, "act", nil)
  history := a.HistorySnapshot(HistoryPolicy{Mode: "LastN", N: 6, IncludeTools: true})
  msgs := append(a.assembleMessages(a.persona, pinned, []string{"progress/plan/current.json"}, phase, "Apply the plan. Use tools as needed. Stop when tests pass or no more actions are required."), history...)
  max := a.runBuds.MaxSteps
  if max <= 0 { max = 6 }
  req := core.Request{
    Messages: msgs,
    Tools: []core.ToolHandle{
      tools.NewCoreAdapter(repoSearch),
      tools.NewCoreAdapter(runTests),
      tools.NewCoreAdapter(jsonValidate),
    },
    ToolChoice: core.ToolChoiceAuto,
    StopWhen: core.Any(
      core.MaxSteps(max),
      stopWhenMaxConsecutiveToolSteps(a.runBuds.MaxConsecutiveToolSteps),
      core.NoMoreTools(),
    ),
  }
  result, err := a.taskRunner.ExecuteRequest(ctx, req)
  if err != nil { return err }
  // Persist a short action log
  _ = os.WriteFile(env.Progress("events.ndjson"), []byte(fmt.Sprintf("{\"steps\": %d}\n", len(result.Steps))), 0o644)
  return nil
}

// runVerify: check acceptance criteria; if fail and budget allows, we could iterate once
func (a *Agent) stepVerify(ctx context.Context, env TaskEnv) error {
  pinned, _ := a.mem.BuildPinned(ctx)
  phase, _ := a.mem.BuildPhase(ctx, "verify", nil)
  history := a.HistorySnapshot(HistoryPolicy{Mode: "Summary", MaxTokens: 200})
  msgs := append(a.assembleMessages(a.persona, pinned, []string{"progress/plan/current.json"}, phase, "Verify acceptance criteria are satisfied. If not, summarize gaps and propose next actions."), history...)
  req := core.Request{ Messages: msgs, MaxTokens: 300 }
  res, err := a.provider.GenerateText(ctx, req)
  if err != nil { return err }
  _ = os.MkdirAll(env.Progress("verify"), 0o755)
  _ = os.WriteFile(env.Progress("verify/0001-report.json"), []byte(fmt.Sprintf("{\n  \"summary\": %q\n}\n", strings.TrimSpace(res.Text))), 0o644)
  return nil
}
```

## Wiring It All Together — DoTask()

The `main` function below wires provider, runner, sandbox, memory channels, approvals, and runs one task end‑to‑end.

```go
package main

import (
  "context"
  "log"
  "os"
  "time"

  "github.com/shillcollin/gai/providers/openai"
  "github.com/shillcollin/gai/runner"
  agentx "github.com/shillcollin/gai/agentx"
)

func main() {
  ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
  defer cancel()

  // Provider + Runner
  p := openai.New(openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")), openai.WithModel("gpt-4o-mini"))
  r := runner.New(p, runner.WithToolTimeout(45*time.Second))

  // Memory + Approvals (conceptual)
  mm := agentx.NewMemoryManager(/* Zep adapter */)
  ab := agentx.NewFileApprovalBroker()

  // Build Agent
  a := agentx.New(agentx.AgentOptions{
    ID: "agent-123",
    Persona: "Pragmatic coding agent. Prefer tests first.",
    Provider: p,
  }, r, mm, ab)

  // Execute one task end-to-end with DoTask
  final, err := a.DoTask(ctx, "./my-task", agentx.TaskSpec{
    Goal: "Fix bug",
    UserID: "user_42",
    RequirePlanApproval: true,
    Budgets: agentx.Budgets{ MaxWallClock: 3 * time.Minute, MaxSteps: 8, StepToolTimeout: 60 * time.Second },
  })
  if err != nil { log.Fatal(err) }
  log.Println("Progress dir:", final.ProgressDir)
}
```

### Alternate Usage: Views and Streaming

```go
// 1) Views: choose how much to return
res, err := a.DoTask(ctx, "./my-task", agentx.TaskSpec{Goal: "Fix bug"}, agentx.WithResultView(agentx.ResultViewSummary))
if err != nil { /* handle */ }
fmt.Println("Summary:", res.OutputText)

// 2) Streaming events during DoTask: provide an event sink
events := make(chan agentx.AgentEvent, 64)
go func() {
  for ev := range events {
    fmt.Println(ev.Type, ev.Step, ev.Message)
  }
}()
res2, err := a.DoTask(ctx, "./my-task", agentx.TaskSpec{Goal: "Fix bug"}, agentx.WithEventSink(events))
close(events)
if err != nil { /* handle */ }
fmt.Println("Finished:", res2.FinishReason.Type)
```
```

## Observability And Journaling

- Stream model events using the runner’s stream APIs if building live UIs.
- Emit agent‑level events (phase start/finish, approvals, memory read/write) to `progress/events.ndjson` and an obs sink.
- Record final usage/cost for budget tracking.

## What To Customize Next

- Replace placeholder tools with real ones: sandboxed `go test`, repo scanners, JSON schema validation.
- Wire the actual MemoryProvider (Zep) and use `MemoryManager.BuildPinned/BuildPhase` and `Remember` writes after Verify.
- Enforce approvals strictly by removing the auto‑approve demo snippet and integrating a Slack/HTTP broker.
- Enrich Verify with acceptance criteria parsing from `task/bug.md` and tool‑based checks.
- Add budgets (tokens/cost/time) and reserve a fraction for Verify.

This example demonstrates the intended developer experience: declarative per‑step prompts and controls, typed tools, multi‑channel memory, approvals, and deterministic filesystem journaling — all built on the stable gai SDK foundations.

# GAI Agent Framework

This document captures the concrete v1 implementation of the GAI Agent Framework. It explains what the agent does, how to wire it into your application, and how to operate it in production.

---

## 1. Overview

The GAI Agent Framework turns a large-language-model provider plus a toolbox into a **governable AI worker**. Key features:

- **Provider agnostic:** Works with any `core.Provider`; swap OpenAI, Anthropic, Gemini, etc.
- **Typed tools & sandboxing:** Execute Go tools locally or inside isolated sandboxes/skills.
- **Governance built-in:** Plan approval, per-tool approvals, budget enforcement, deterministically journaled state.
- **Memory integration:** Strategy-driven memory manager with optional Zep adapter.
- **Observability:** Structured `gai.agent.events.v1` NDJSON stream, OTEL metrics, and resumable checkpoints.
- **HTTP/CLI surface:** Simple REST API and `gai-agent` CLI for orchestration.

This doc assumes familiarity with the base `gai` SDK (providers, tools, runner, sandbox).

---

## 2. Getting Started

```bash
go get github.com/shillcollin/gai/agentx
```

### Minimal Go Example

```go
package main

import (
  "context"
  "log"
  "time"

  "github.com/shillcollin/gai/agentx"
  "github.com/shillcollin/gai/core"
  "github.com/shillcollin/gai/providers/openai"
  "github.com/shillcollin/gai/runner"
)

func main() {
  ctx := context.Background()

  provider := openai.New(
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    openai.WithModel("gpt-4o-mini"),
  )

  agent, err := agentx.New(agentx.AgentOptions{
    ID:           "worker-1",
    Persona:      "You are a meticulous research assistant.",
    Provider:     provider,
    Tools:        []core.ToolHandle{}, // add tools as needed
    DefaultBudgets: agentx.Budgets{
      MaxWallClock: 3 * time.Minute,
      MaxSteps:     8,
      MaxTokens:    8000,
    },
    RunnerOptions: []runner.RunnerOption{runner.WithToolTimeout(45 * time.Second)},
  })
  if err != nil { log.Fatal(err) }

  taskRoot := "/tmp/my-task" // contains task/ and progress/ directories
  result, err := agent.DoTask(ctx, taskRoot, agentx.TaskSpec{Goal: "Summarize the latest AI safety news"})
  if err != nil { log.Fatal(err) }

  log.Printf("Finished with reason %s. Progress dir: %s", result.FinishReason.Type, result.ProgressDir)
}
```

At the end of execution the agent writes artifacts under `progress/` and returns a `DoTaskResult`.

---

## 3. Task Workspace Layout

Each task lives in a root directory with two subfolders:

```
<root>/
  task/        # read-only inputs provided by humans/systems
  progress/    # agent write volume (notes, plan, artifacts, state)
```

The agent never modifies `task/`. All journaling, approvals, and artifacts land in `progress/`:

- `notes.md`, `todo.md`, `findings.md`
- `plan/current.json` + versioned copies
- `verify/*.md`
- `events.ndjson` (`gai.agent.events.v1`)
- `state.json` (`agent.state.v1` checkpoint)
- `approvals/{requests,decisions}/`
- `steps/<step_id>/` step outputs

`DoTask` guarantees atomic writes (`writeAtomic`) so readers never see partial files.

---

## 4. AgentOptions Reference

| Field | Purpose |
|-------|---------|
| `ID` | Stable agent identifier used in state/events. |
| `Persona` | System prompt prefix applied to every phase. |
| `Provider` | Implementation of `core.Provider`. |
| `Tools` | Tool handles available during Act. |
| `Skills` | Optional sandboxed skills (`SkillBinding{Name, Skill, Assets}`). First skill is auto-bound to the runner when a sandbox manager is supplied. |
| `SandboxManager` / `SandboxAssets` | Configure sandbox session provisioning for tools requiring isolation. |
| `Memory` | Custom memory manager implementing `MemoryManager`. Nil => default strategy manager with `memory.NullProvider`. |
| `Approvals` | Default policy for plan/tool approvals. |
| `DefaultBudgets` | Ceiling for wall-clock, steps, tokens, cost, consecutive tool steps. |
| `RunnerOptions` | Extra runner options (timeouts, interceptors). |
| `ApprovalBrokerFactory` | Build a broker per task root (default: file-based). |
| `Observability`, `SeedMessages`, `DefaultResultView` | Optional extras (OTEL config, seed history, default result projection). |

`TaskSpec` mirrors many of these knobs per-task (goal, user ID, budgets, approvals, seed messages).

---

## 5. Execution Model (Gather → Plan → Act → Verify)

1. **Gather**
   - Reads `task/` inputs, calls the provider for findings, writes `progress/findings.md`, updates `notes.md`.
   - Injects memory (`pinned` + `gather` phase) before prompting.

2. **Plan**
   - Loads or generates `PlanV1` (`goal`, `steps`, `allowed_tools`, `acceptance`).
   - Writes `plan/current.json`, computes `plan_sig`, records in state. Plan approvals recorded via `progress/approvals/*`.

3. **Act**
   - Runs `runner.ExecuteRequest` with tool gating (`allowed_tools`).
   - Tool approvals enforced via `approvalToolHandle`.
   - Journals step outputs into `progress/steps/`.

4. **Verify**
   - Evaluates acceptance criteria, writing `verify/0001-report.md`.

Each phase emits `phase.start`/`phase.finish` events and updates `state.json`. If a run stops mid-phase, the agent resumes from the recorded phase on the next `DoTask` invocation.

### Resume Semantics

- If `progress/state.json` exists and `phase != "finished"`, the agent replays remaining phases.
- Plan signatures are reused by reading `plan/current.json` to avoid regenerating plans during resume.

---

## 6. Memory

A strategy-driven manager composes memory snippets.

```go
mgr := memory.NewManager(memory.Strategy{
  Pinned: memory.PinnedStrategy{Enabled: true, Template: "Pinned:\n{{content}}"},
  Phases: map[string]memory.PhaseStrategy{
    "gather": {Queries: []memory.Query{{Name: "recent_findings", TopK: 3}}},
    "act":    {Queries: []memory.Query{{Name: "prior_actions", TopK: 5}}},
  },
  Dedupe: memory.DedupeStrategy{ByTitle: true},
}, memory.NullProvider{}, memory.Scope{Kind: "agent", ID: "agent-123"})

agent, _ := agentx.New(agentx.AgentOptions{
  // ...
  Memory: mgr,
})
```

Implement `memory.Provider` to back queries with your own store. The optional `memory/zep_adapter.go` (build tag `zep`) shows how to hydrate pinned/phase memory via the Zep Go SDK.

Errors during retrieval trigger `type="error"` events but do not abort the run.

---

## 7. Tools, Skills & Sandbox

- **Tools** (`core.ToolHandle`): run inline during Act. The agent wraps each tool with `approvalToolHandle` to enforce plan/approval policies.
- **Skills** (`SkillBinding`): pair a `*skills.Skill` manifest with assets. The framework binds the first skill automatically when `SandboxManager` is set.
- **Sandbox**: call `sandbox.NewManager` with a long-lived context. Budgets/timeouts inherit from `SessionSpec` in the skill manifest.

Example skill binding:

```go
skill, _ := skills.NewFromPath("agents/python-sandbox/skill.yaml")
manager, _ := sandbox.NewManager(ctx, sandbox.ManagerOptions{})

agent, _ := agentx.New(agentx.AgentOptions{
  // …
  Skills:         []agentx.SkillBinding{{Name: "python", Skill: skill}},
  SandboxManager: manager,
})
```

If you need multiple skills or dynamic selection, spin up separate agents or extend the binding logic.

---

## 8. Approvals

- **File broker (default):** writes JSON requests under `progress/approvals/requests/` and waits for decisions under `.../decisions/`.
- **Plan approvals:** triggered after plan generation when policy requires `RequirePlan=true` or `spec.RequirePlanApproval`.
- **Tool approvals:** per-tool gating (`ApprovalPolicy.RequireTools`).
- **Custom brokers:** implement `ApprovalBroker` and set `AgentOptions.ApprovalBrokerFactory` (e.g., Slack/HTTP service).
- **Web UI:** the HTTP server exposes `/agent/tasks/approvals?root=<path>`; visit in a browser to view pending requests and click Approve/Deny (JSON API available when `Accept: application/json`).

Request schema:

```json
{
  "id": "req_123",
  "type": "plan",
  "plan_path": "progress/plan/current.json",
  "plan_sig": "…",
  "expires_at": "2025-02-01T12:00:00Z"
}
```

---

## 9. Budgets & Stop Reasons

Budgets enforce runtime guardrails and propagate into events/result.

| Budget | Enforcement |
|--------|-------------|
| `MaxWallClock` | Wraps `DoTask` context with timeout. |
| `MaxSteps` | Added to runner stop conditions. |
| `MaxConsecutiveToolSteps` | Custom stop condition (`stopWhenMaxConsecutiveToolSteps`). |
| `MaxTokens` | Checked after each provider call; emits `budget.max_tokens`. |
| `MaxCostUSD` | Same as tokens with currency guard. |

`usage.delta` events include `budgets_remaining` metadata for dashboards/alerts.

---

## 10. Events & Observability

- Events follow `gai.agent.events.v1` and are written to `progress/events.ndjson`.
- Types include `phase.start`, `phase.finish`, `approval.requested`, `approval.decided`, `memory.error`, `usage.delta`, `finish`.
- Forward events to your telemetry pipeline (SSE/stream). The HTTP API exposes `/agent/tasks/events` for tailing.
- Metric hooks leverage `obs` (request counts, latencies, token histograms). `usage.delta` feeds budget dashboards.

Example event:

```json
{"version":"gai.agent.events.v1","id":"ev_01H...","type":"usage.delta","ts":1732740051000,
 "data":{"total_tokens":150,"budgets_remaining":{"tokens":7850}}}
```

---

## 11. HTTP API

The `agentx/httpapi` package exposes a simple REST server:

| Endpoint | Description |
|----------|-------------|
| `POST /agent/tasks/do` | Run a task synchronously. |
| `POST /agent/tasks/do/async` | Start a background run. |
| `GET /agent/tasks/{id}/result` | Fetch cached result. |
| `GET /agent/tasks/events?root=<path>` | Stream NDJSON event feed. |

`DoTaskRequest` JSON:

```json
{
  "root": "/path/to/task-root",
  "spec": { "goal": "Fix bug" },
  "options": { "result_view": "full" }
}
```

Use the server in combination with the CLI or external orchestrators (e.g., Airflow, Temporal).

---

## 12. CLI (`gai-agent`)

Commands:

| Command | Flags | Description |
|---------|-------|-------------|
| `run` | `--server`, `--root`, `--goal`, `--spec`, `--result-view` | Invoke `/agent/tasks/do`. |
| `tail` | `--server`, `--root` | Stream events via `/agent/tasks/events`. |
| `resume` | `--server`, `--root`, `--goal`, `--spec` | Kick off `/agent/tasks/do/async` for a partially complete task. |

Example:

```bash
gai-agent run --server http://localhost:8080 --root /tmp/my-task --goal "Prepare weekly status report"
gai-agent tail --server http://localhost:8080 --root /tmp/my-task
```

---

## 13. Resume & State (`agent.state.v1`)

`state.json` persists:

```json
{
  "version": "agent.state.v1",
  "phase": "plan",
  "plan_sig": "…",
  "budgets": {"max_wall_clock_ms": 180000, ...},
  "usage": {"total_tokens": 150},
  "started_at": 1732740000000,
  "updated_at": 1732740030000
}
```

`DoTask` reads this file, resumes from the stored phase, and refuses to rerun if `phase == "finished"`.

---

## 14. Extending the Framework

- **Custom memory**: implement `MemoryManager` or provide a custom `Provider` that queries your vector DB.
- **Broker integrations**: plug Slack/email approvals by providing a broker factory.
- **Additional skills**: extend `AgentOptions.Skills` list and customize runner wiring if you need more than one skill.
- **Observability**: subscribe to events, push them into your telemetry stack, and extend `obs` metrics as needed.

### Example Project

The repository ships an end-to-end demo at `examples/demo/lds_report` that researches the LDS church, writes a report, and produces a PDF via the Python sandbox skill. Run it with:

```bash
OPENAI_API_KEY=... go run ./examples/demo/lds_report
```

Inspect `task/brief.md` for the prompt, and browse `progress/` after the run to see the generated plan, verification report, and PDF artifact.

---

## 15. Appendix: Useful Types

### PlanV1

```go
type PlanV1 struct {
  Version      string   `json:"version"`
  Goal         string   `json:"goal"`
  Steps        []string `json:"steps"`
  AllowedTools []string `json:"allowed_tools"`
  Acceptance   []string `json:"acceptance"`
}
```

### DoTaskResult

```go
type DoTaskResult struct {
  AgentID      string
  TaskID       string
  ProgressDir  string
  OutputText   string
  Usage        core.Usage
  FinishReason core.StopReason
  Deliverables []DeliverableRef
  Steps        []StepReport
  Warnings     []core.Warning
}
```

### AgentEventV1

```go
type AgentEventV1 struct {
  Version string         `json:"version"`
  ID      string         `json:"id"`
  Type    string         `json:"type"`
  Ts      int64          `json:"ts"`
  Step    string         `json:"step,omitempty"`
  Message string         `json:"message,omitempty"`
  Data    map[string]any `json:"data,omitempty"`
  Error   string         `json:"error,omitempty"`
}
```

---

**Next steps:**

- Use the HTTP server + CLI for orchestration.
- Integrate Zep or another memory provider if you need rich recall.
- Extend sandbox/skills to fit your toolchain (e.g., containerized code execution).

For questions or contributions, open an issue or PR in the main repository.

# GAI Agent Framework — Design Report

This document proposes a full‑stack Agent framework built on top of the existing gai SDK. It consolidates our current thinking, details the architecture, and outlines a default agent with safe, governable behaviors. It is intentionally thorough to serve as a design and implementation reference.

## Executive Summary

- Goal: Provide a production‑grade Agent that plans, acts with tools/skills, observes results, verifies outcomes, and persists state — all provider‑agnostic, strongly typed, observable, and safe by default.
- Key choices:
  - Two work folders per task: `task/` (inputs, read‑only) and `progress/` (agent working area, read/write).
  - Memory injection via a curated “Memory” message plus phase‑specific retrieval during the loop (both supported, configurable).
  - Default loop: Gather → Plan → Act → Verify with clear stop conditions, budgets, and approvals.
  - Approvals: file‑based broker (MVP) for plan and tool calls; policy driven and auditable.
- Memory: pluggable provider interface; start with Zep. The framework stays agnostic to storage schema.
  - Reuse existing gai primitives: `runner` for multi‑step + tools, `skills` + `sandbox` for isolation, `obs` for events, `prompts` for templating.

## Scope And Non‑Goals

- In scope: single‑agent orchestration, tools/skills integration, approvals, journaling, resumability, memory separation, and observability.
- Not in scope (initially): multi‑agent negotiation, fully automated skill creation (tracked as TODO), complex workflow engines (lives outside the Agent).

## Core Concepts

- Agent: long‑lived orchestrator with identity, policy, loop, state, and IO (tools/skills/memory/approvals/events).
- Task: a scoped execution with its own `task/` inputs and `progress/` working area.
- Memory vs Task Context:
  - Memory: durable cross‑task knowledge (facts, episodic events, semantic embeddings, procedures/playbooks).
  - Task Context: ephemeral, task‑specific working set (plan, findings, artifacts, to‑dos).
- Messages: provider‑agnostic, multimodal transcript used to prompt models (`core.Message`), assembled deterministically.
- Tools vs Skills: typed tools run locally or in sandbox; skills are versioned, sandboxed bundles with dedicated instructions and mounts.
- Approvals: policy‑driven gates for plans, tool calls, budgets, and credentials.

## Identity And Persona

- Identity: `agent_id` (durable), friendly name, and capabilities.
- Persona: system‑level instructions reflecting tone, goals, domain focus, and constraints.
- Policy: safety/auth policies, domain allowlists, budgets, and approval requirements.

## Task Directory Model

We standardize two directories per task root to make behavior predictable and human‑friendly.

- `task/` (read‑only for the Agent): inputs provided by external systems or humans. The Agent never writes here.
- `progress/` (read/write): the Agent’s working surface and paper trail. Humans may edit files here to steer execution.

Recommended initial structure (only `notes.md`, `todo.md`, and `state.json` are considered baseline; others are optional and created as needed):

- `progress/notes.md` — running log; succinct updates per phase.
- `progress/todo.md` — simple checklists for the task.
- `progress/state.json` — current phase, budgets used, last updated, status.
- `progress/plan/current.json` — the latest plan; versioned copies in `progress/plan/0001.json`, `0002.json`, etc.
- `progress/findings.md` — curated facts discovered during Gather.
- `progress/events.ndjson` — append‑only journal of agent events.
- `progress/sources.jsonl` — citations (url/title/snippet/checksum) for provenance.
- `progress/artifacts/` — downloaded or generated artifacts.
- `progress/verify/0001-report.json` — verification reports.
- `progress/approvals/requests/*.json` — pending approval requests.
- `progress/approvals/decisions/*.json` — human/service decisions.

Rationale: human‑readable where it helps (markdown), machine‑friendly where structure matters (JSON/JSONL). All files are optional except a minimal trio (`notes.md`, `todo.md`, `state.json`).

## Loop Model

The loop is pluggable (function), but we ship a safe, useful default: Gather → Plan → Act → Verify.

- Gather
  - Purpose: explore surroundings and inputs; form an understanding of the task space.
  - Inputs: `task/` files, memory (phase retrieval), available tools (web, repo/file introspection, etc.).
  - Outputs: `progress/findings.md`, `progress/artifacts/*`, `progress/sources.jsonl`, updates to `progress/notes.md` and `progress/todo.md`.
  - Stop: time‑box (budget share), diminishing returns, or approvals needed.

- Plan
  - Purpose: produce a structured plan (`progress/plan/current.json`) with steps, objectives, tools to use, constraints, and acceptance criteria mapping.
  - Approval: if policy requires, write an approval request and pause until approved.

- Act
  - Purpose: execute steps via `runner`, calling tools/skills; persist tool I/O summaries and artifacts.
  - Safety: respect approvals, budgets, tool caps (including consecutive tool‑call cap), domain/file allowlists.
  - Persistence: tool events and summaries to `progress/events.ndjson`; key outputs to `progress/artifacts/`.

- Verify
  - Purpose: evaluate outputs vs acceptance criteria; optionally run tests/checks; record report. If failed and budget remains, refine plan and iterate once (policy‑controlled).
- Memory write: at successful completion (or at budget end), summarize key learnings with provenance.

Stop conditions combine existing runner conditions with agent‑level budgets:

- Max total steps, max consecutive tool‑call steps, no more tools, max wall‑clock, explicit STOP file (`progress/STOP`).

## Memory System

We separate durable memory from task context. Memory is pluggable and multi‑channel so the Agent can compose a view from multiple subjects (Agent, User, Org/Project, Task). The framework does NOT prescribe lane types (e.g., facts vs semantic); those are up to providers and applications. We focus on how memory is selected and injected, not how it is stored.

- MemoryProvider (abstraction):
  - Store and retrieve entries tagged with metadata (agent_id, task_id, channel scope, provider‑defined tags).
  - Provide retrieval hooks for per‑phase injection (e.g., “user preferences”, “prior similar tasks”).
  - Providers may implement KV, vector search, or any hybrid; the framework stays agnostic.

- Injection strategies (configurable, per channel):
  - Pinned “Memory” message: compact, curated, stable; always present near the system persona.
  - Phase injection: targeted retrieval appended as distinct sections during Gather/Plan/Verify based on provider‑agnostic queries.

- Zep as initial provider:
  - The Zep adapter can map entries to KV and vector collections internally, but this is not required by the framework.

### Memory Channels And Views

Memory is organized as multiple channels keyed by subject, then composed into a view per task/session.

- Channels (scopes):
  - Agent channel: `agent:{agent_id}` — persona facts, capabilities, recurring strategies.
  - User channel: `user:{user_id}` — preferences, history, constraints.
  - Org/Team/Project channels: `org:{org_id}`, `team:{team_id}`, `project:{project_id}` — policies, style guides.
  - Task channel: `task:{task_id}` — ephemeral findings/outcomes for the active task.

- Scoping and precedence (defaults):
  - Compose view as [Agent, User, Org/Project, Task].
  - Preference conflicts: User > Org/Project > Agent.
  - Capability/constraint conflicts: Agent > Org/Project > User (configurable).

- Injection budget defaults (per phase, per channel; configurable):
  - Pinned: Agent ≤ 80 tokens, User ≤ 60 tokens, Org/Project ≤ 60 tokens.
  - Gather recalls: Agent k=4, User k=3, Org/Project k=2 (query names provider‑defined).
  - Plan recalls: Agent k=3, User k=3 (focus on preferences/constraints).
  - Verify recalls: Org/Project k=3 (policies/acceptance), Agent k=2.

- Privacy & approvals:
  - Policy may require approvals to read/write certain channels (e.g., Org memory).
  - Redact PII in pinned blocks; cite `progress/sources.jsonl` rather than dumping raw content.
  - Journal `memory.read`/`memory.write` events with scope/tags/counts (not raw payloads).

Conceptual types (framework‑agnostic storage; providers define their own tags/queries):

```go
type MemoryScope struct {
  Kind    string // "agent"|"user"|"org"|"team"|"project"|"task"
  ID      string // scope identifier
  AgentID string // multi-tenant tag
}

type MemoryQuery struct {
  Name      string            // provider/application-defined label (e.g., "agent_facts", "user_prefs")
  Filter    map[string]any    // optional provider-defined filters (e.g., tags)
  TopK      int
  MaxTokens int
}

type PhaseInjection struct {
  Queries []MemoryQuery
}

type InjectionStrategy struct {
  Pinned struct{ Enabled bool; MaxTokens int; Template string }
  Phases map[string]PhaseInjection // keys: "gather","plan","verify"
  Redact bool
  Dedupe struct{ ByHash, ByTitle bool }
}

type MemoryBinding struct {
  Scope    MemoryScope
  Strategy InjectionStrategy
  Weight   float32
  Enabled  bool
}
```

## Task Context

Task Context is the Agent’s ephemeral working set distinct from memory:

- Contents: findings, plan, intermediate JSON objects, artifacts, TODOs, citations.
- Storage: the `progress/` folder. The loop and tools write and read here consistently.
- Prompt rendering: a context renderer assembles a deterministic prompt segment from select files (e.g., `findings.md`, `plan/current.json`) separate from memory segments.

## Messages And Prompt Assembly

We continue using `core.Message` and multimodal `Part`s. Assembly is deterministic, memory‑aware, and history‑aware:

1. Persona system message (Agent identity and instructions).
2. Pinned memory blocks per active channel (e.g., Agent facts, User preferences), curated and small.
3. Task Context segment (rendered `progress/findings.md`, selected highlights from artifacts, plan summary).
4. Phase‑specific memory retrieval sections per channel (e.g., “Relevant Memory for Planning”).
5. Seeded conversation history (optional; see below) rendered according to the step’s history policy.
6. User request (from `task/` or API) and any step‑local prompt parts.

Provider‑specific tweaks happen through typed provider options and runner behaviors, not by mutating the above order.

### Seeding Conversation History

Agents often need to start with prior conversation context or carry a slice of history between steps. We keep this explicit and policy‑driven to manage cost and determinism.

- Sources of seed history:
  - Programmatic: `AgentOptions.SeedMessages []core.Message` (Agent‑wide defaults) and `TaskSpec.SeedMessages []core.Message` (per task).
  - References: `TaskSpec.SeedFromSessions []string` to rehydrate prior transcripts from a store, rather than pasting raw messages.
  - Files: optional `task/history.jsonl` (list of messages) or `task/history.md` (annotated), parsed by a helper.

- History policy (per step):
  - None: ignore history for this step.
  - SeedOnly: include seed messages, but do not carry intra‑run messages.
  - LastN(n): include the last N messages from the active session (assistant/user/tool) in addition to seeds.
  - Summary(maxTokens): include a generated session summary (system message) bounded by tokens, instead of raw turns.
  - IncludeTools: whether tool call/response parts are kept when rendering.

- Placement: seeded history is injected after memory/context segments and before the step’s new user ask. This keeps the persona + pinned memory stable for caching, while giving the model relevant conversational state.

- Budget & caching guidance:
  - Prefer Summary for long conversations to stabilize prompts and preserve prefix caching.
  - Keep seeds small and stable; avoid updating them every step.
  - History rendering should dedupe against Task Context to prevent repeated content.

- APIs (conceptual):
  - `AgentOptions.SeedMessages []core.Message`
  - `TaskSpec.SeedMessages []core.Message`
  - `StepSpec.History Policy` with fields `{Mode: None|SeedOnly|LastN|Summary, N: int, MaxTokens: int, IncludeTools: bool}`
  - Methods: `SeedHistory(msgs ...core.Message)`, `ClearHistory()`, `HistorySnapshot()`

## Tools And Skills

- Tools: typed `core.ToolHandle`s with timeouts, parallelism caps, and per‑tool policies. Recommended defaults:
  - Gather: `web_search`, `url_extract`, `http_fetch`, `file_glob`, `file_read`, simple repo scan.
  - Act: `sandbox_exec` (scoped to sandbox), `fs_write`/`fs_read` restricted to `progress/`.
  - Verify: `json_validate`, `links_check`, optional `test_runner` when code artifacts exist.

- Skills: versioned bundles managed by `agent.Bundle` and `skills` + `sandbox`.
  - We already support bundles and skill configs (see examples at `examples/skills/sandbox_agent/main.go:19`).
  - Dynamic skill creation by the agent is a TODO to be designed with approvals.

## Approvals (v1)

Simplified, pragmatic scope for v1. We keep policy simple and the broker file‑based under `progress/approvals/`.

- Policy knobs:
  - Plan approval (bool): if required, gate between Plan → Act.
  - Tool approvals (list of names): if a tool’s name appears in the list, each invocation requires approval.

- Request files (Agent writes): `progress/approvals/requests/<id>.json`

```json
{
  "id": "req_01H...",
  "type": "plan|tool",
  "plan_path": "progress/plan/current.json",
  "tool_name": "sandbox_exec",
  "rationale": "why this is needed",
  "expires_at": "2025-01-01T12:00:00Z"
}
```

- Decision files (Human/service writes): `progress/approvals/decisions/<id>.json`

```json
{
  "id": "req_01H...",
  "decision": "approved|denied|expired",
  "approved_by": "user@example.com",
  "timestamp": "2025-01-01T12:05:00Z"
}
```

- Behavior:
  - Plan: after writing `progress/plan/current.json`, write one request and wait for decision or timeout.
  - Tools: before invoking a tool whose name is in the require‑approval list, write a request and wait.
  - Dev mode: Auto‑approve is allowed for local development; still write requests/decisions for audit.
  - Journaling: mirror requests/decisions to `progress/events.ndjson` and emit obs events.

Recommended defaults:
- Strict: `RequirePlan=true`, `RequireTools=["sandbox_exec","http_post","fs_write"]`.
- Permissive: `RequirePlan=false`, `RequireTools=[]`.

Future extensions (not in v1): argument/domain matchers, budget approvals, credential scope approvals, batched allowances, and constraints. The file schema above can be extended with optional fields later without breaking v1.

## Auth And Credentials

- Credential broker: per‑tool credential scoping; sources include env, Vault, or callbacks.
- Scoped exposure: credentials are mounted only during tool execution (env or files in sandbox) and never written to `progress/`.
- Redaction: inputs/outputs are redacted in logs/obs/events.

## Budgets And Stop Conditions

- Minimal v1 budgets are enforced per task and combined with runner stop conditions:
  - Max wall‑clock: wrap DoTask context with a timeout.
  - Max total steps: use `core.MaxSteps(N)`.
  - Max consecutive tool‑call steps: add a stop condition (see example in docs/AGENT_EXAMPLE.md).
  - Step tool timeout: construct the per‑task runner with `runner.WithToolTimeout`.
- Reserve budget for Verify (optional) to avoid starving the final phase.

## Observability And Journaling

- Build on `obs.LogCompletion` and add Agent‑level events: phase start/finish, plan proposed/approved, tool approval requested/decided, memory read/write, verify pass/fail, budget hit.
- Local journal: append to `progress/events.ndjson` to enable replay and post‑mortems.
- Streaming: for UIs, mirror agent events alongside model stream events (see NDJSON stream in `apps/webdemo/backend/streaming.go:36`).

### Agent Events

As DoTask runs, the Agent emits a normalized event stream in parallel to model events.

```go
type AgentEvent struct {
  ID        string
  Type      string      // e.g., "phase.start", "phase.finish", "approval.requested", "approval.decided", "memory.read", "memory.write", "tool.call", "tool.result", "verify.pass", "verify.fail", "finish", "error"
  Step      string
  Timestamp int64
  Message   string
  Data      map[string]any
  Error     string
}
```

Events are appended to `progress/events.ndjson` and can be streamed via an HTTP endpoint. They are also mirrored to `obs` sinks as needed.

## State, Persistence, And Resume

- Journal and checkpoints: serialize the current phase, plan hash, budgets, last event seq, and recent state to `progress/state.json`.
- Resume: Agent can rehydrate from `task/` + `progress/` and continue from the last checkpoint.

### On‑Disk Step Layout

- `progress/steps/<step_id>/` holds step‑scoped outputs to keep artifacts and reports tidy even for custom loops. Typical files:
  - `output.json` (or `output.md`) — primary step output
  - `artifacts/` — any files produced by the step
  - `notes.md` — optional step notes
  - `events.ndjson` — tool call summaries for this step

`StepReport.OutputRef` points to the primary output path; `ArtifactsWritten` lists additional files. This layout keeps humans and services aligned regardless of loop shape.

Writes to `progress/` should be atomic: write to a temporary file (e.g., `.tmp`) and then rename to the final path to avoid partial reads. Update `progress/state.json` at step boundaries so `Resume(root)` can rehydrate and continue safely.

## Safety And Policy

- Policy engine governs approvals, tool domains, file write scopes, concurrency, and content policies.
- Sensible defaults:
  - sandbox_exec: require approval unless read‑only or confined to `progress/`.
  - http_post: require approval unless domain allowlisted.
  - fs_write: restrict to `progress/` by default.

## Proposed API Surface (Conceptual)

This is illustrative — we will refine names and packages during implementation.

```go
// Agent construction (v1). AgentOptions set agent-wide defaults; TaskSpec can override per task.
type AgentOptions struct {
  ID            string
  Persona       string
  Provider      core.Provider
  Tools         []core.ToolHandle
  Skills        []SkillBinding
  Memory        MemoryProvider // shared backend (Zep); multi-channel via MemoryManager
  Approvals     ApprovalPolicy // v1: RequirePlan bool, RequireTools []string; file-based broker
  Loop          LoopFunc       // optional custom loop; defaults to Gather→Plan→Act→Verify
  Observability obs.Options
  SeedMessages  []core.Message // agent-wide seed history (optional)

  // Optional defaults; TaskSpec can override
  DefaultResultView ResultView
  DefaultBudgets    Budgets // used only if TaskSpec omits budgets (v1 minimal fields)
}

type Agent interface {
  // High-level entry: prepare, run loop, finalize. View controls payload size.
  DoTask(ctx context.Context, root string, spec TaskSpec, opts ...DoTaskOption) (DoTaskResult, error)

  // Lower-level control
  StartTask(ctx context.Context, root string, spec TaskSpec) (TaskHandle, error)
  Run(ctx context.Context, task TaskHandle) (DoTaskResult, error)
  StepOnce(ctx context.Context, task TaskHandle) (StepResult, error)
  Stop(ctx context.Context, task TaskHandle) error
  Resume(ctx context.Context, root string) (TaskHandle, error)

  // Context & memory
  PushContext(ctx context.Context, task TaskHandle, items ...any) error
  Remember(ctx context.Context, scope MemoryScope, entry MemoryEntry, opts MemoryWriteOptions) error
  Recall(ctx context.Context, view []MemoryBinding, q MemoryQuery, opts MemoryReadOptions) (Results, error)
  AttachUser(userID string, strategy InjectionStrategy) error
  DetachUser(userID string) error
  SetMemoryView(view []MemoryBinding, weights map[MemoryScope]float32) error

  // Tools/skills lifecycle
  RegisterTool(handles ...core.ToolHandle)
  RegisterSkill(skill SkillBinding)

  // Policy/approvals
  SetPolicy(ApprovalPolicy)
  SubscribeEvents(ch chan AgentEvent)
}
```

### Wiring Helpers (optional)

Convenience helpers may be provided to configure the Agent without verbose boilerplate:

- `WithRunner(*runner.Runner)`
- `WithSandboxManager(manager, assets)`
- `WithZepMemory(apiKey)`
- `WithFileApprovals()`

Callers can use these or pass equivalents directly via AgentOptions.

### Client API Preview (TS/HTTP SDKs)

Minimal endpoints to support a thin TS SDK and remote orchestration:

- POST `/agent/tasks/do` — DoTask with `view` selector → DoTaskResult
- POST `/agent/tasks/do/async` — start DoTask → `{task_id, stream_token?}`
- GET `/agent/tasks/{id}/events` — NDJSON or SSE AgentEvent stream
- GET `/agent/tasks/{id}/result` — DoTaskResult
- POST `/agent/approvals/{id}/decision` — approve/deny (v1 plan/tool)
- Optional: POST `/agent/tasks/{id}/upload` — upload files into `task/`

## Result Types & Views

We keep the top-level return typed and flexible for custom loops and step designs.

```go
// ResultView controls how much is returned (and serialized)
type ResultView int
const (
  ResultViewMinimal ResultView = iota // task handle, progress dir, finish reason, usage
  ResultViewSummary                   // + short output, key deliverables, verify status
  ResultViewFull                      // + step reports, tool calls, warnings
)

type DoTaskOption interface{}
// Common options: WithResultView(ResultView), WithEventSink(ch chan AgentEvent), WithExt(map[string]any)

// Top-level result – loop-agnostic, step-flexible
type DoTaskResult struct {
  AgentID      string
  TaskID       string
  LoopName     string
  LoopVersion  string
  LoopSignature string // hash of step graph/spec used

  ProgressDir  string
  Deliverables []DeliverableRef
  OutputText   string // optional convenience (summary or primary deliverable)

  Usage        core.Usage
  FinishReason core.StopReason
  Warnings     []core.Warning

  Steps        []StepReport // may be omitted in Minimal/Summary views

  StartedAt    int64 // UTC millis
  CompletedAt  int64 // UTC millis

  Ext          map[string]any // escape hatch for custom orchestration
}

type DeliverableRef struct {
  Path   string // relative to task root
  Kind   string // e.g., "report", "artifact", "code"
  Title  string
  MIME   string
  Size   int64
  SHA256 string
}

type StepReport struct {
  ID        string // stable per run (e.g., name+iteration)
  Name      string // e.g., "gather", "plan", or custom
  Kind      string // freeform taxonomy: "phase", "custom:triage" etc.
  Iteration int
  Status    string // completed|failed|skipped

  StartedAt int64
  EndedAt   int64

  ToolsAllowed []string
  ToolsUsed    []string
  ToolCalls    []obs.ToolCallRecord

  ContextRead      []string         // paths under task/ or progress/
  ArtifactsWritten []DeliverableRef // files created under progress/

  OutputRef   string          // path to primary step output under progress/steps/<id>/
  OutputJSON  json.RawMessage // optional structured output
  SchemaID    string          // optional schema identifier (name@version or URI)

  Usage   core.Usage
  Error   string
  Ext     map[string]any
}
```

Notes:
- Loops can define any step graph; StepReport captures what actually happened.
- The on‑disk convention mirrors this via `progress/steps/<step_id>/...`.
- Orchestrators can request Minimal/Summary/Full or project a custom shape via `DoTaskCollect[T]`.

## Minimal Supporting Concepts (v1)

- TaskSpec (per task overrides and inputs):
  - Goal string
  - UserID string
  - SeedMessages []core.Message
  - Budgets Budgets // v1: MaxWallClock, MaxSteps, MaxConsecutiveToolSteps, StepToolTimeout
  - Approvals ApprovalPolicy // optional per-task override
  - RequirePlanApproval bool // convenience (sets Approvals.RequirePlan)

- Budgets (v1 minimal):
  - MaxWallClock time.Duration
  - MaxSteps int
  - MaxConsecutiveToolSteps int
  - StepToolTimeout time.Duration

- ApprovalPolicy (v1):
  - RequirePlan bool
  - RequireTools []string // names requiring approval per call

- LoopFunc: signature that receives `AgentState` and yields next action (plan/tool calls/memory ops/verify/escalate). Optional for v1 deployments.

- MemoryProvider: generic CRUD + query; Zep adapter first.

- ApprovalBroker: file-based MVP that persists requests/decisions as documented above.

Conceptual memory manager types (illustrative):

```go
// Backend abstraction (e.g., Zep adapter)
type MemoryProvider interface {
  Put(ctx context.Context, scope MemoryScope, entry MemoryEntry) error
  Query(ctx context.Context, scopes []MemoryScope, q MemoryQuery) (Results, error)
}

// Generic entry type; providers decide how to persist (KV, vector, hybrid)
type MemoryEntry struct {
  Data     any
  Tags     []string
  Metadata map[string]any // provenance, confidence, ttl, etc.
}

// Runtime composition for a task/session
type MemoryManager interface {
  Attach(binding MemoryBinding)
  Detach(scope MemoryScope)
  SetView(bindings []MemoryBinding, weights map[MemoryScope]float32)

  BuildPinned(ctx context.Context) ([]core.Message, error)
  BuildPhase(ctx context.Context, phase string, taskCtx any) ([]core.Message, error)
}
```

## Example: Building A Simple GAI Agent

This example sketches usage with proposed shapes; it references existing packages for providers, runner, sandbox, and obs. It is illustrative — exact types will evolve.

```go
package main

import (
  "context"
  "log"
  "time"

  "github.com/shillcollin/gai/core"
  "github.com/shillcollin/gai/providers/openai"
  "github.com/shillcollin/gai/runner"
  "github.com/shillcollin/gai/sandbox"
  "github.com/shillcollin/gai/tools"
  // agent package to be introduced in this repo
  agent "github.com/shillcollin/gai/agentx"
)

func main() {
  ctx := context.Background()

  // Provider
  p := openai.New(openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")), openai.WithModel("gpt-4o-mini"))

  // Tools (examples):
  exec := tools.NewSandboxCommand("sandbox_exec", "Execute shell", []string{"/bin/sh", "-lc", ""})
  fetch := tools.NewHTTPFetch()

  // Sandbox manager for skills/tools that require isolation
  mgr, _ := sandbox.NewManager(ctx, sandbox.ManagerOptions{})

  // Defaults: Zep memory provider, file-based approvals, default loop
  a := agent.New(agent.AgentOptions{
    ID:       "agent-123",
    Persona:  "You are a helpful research and coding agent. Be concise and cite sources.",
    Provider: p,
    Tools:    []core.ToolHandle{exec, fetch},
  },
    agent.WithRunner(runner.New(p, runner.WithToolTimeout(60*time.Second))),
    agent.WithSandboxManager(mgr),
    agent.WithZepMemory(os.Getenv("ZEP_API_KEY")),
    agent.WithFileApprovals(),
  )

  // Multi-channel memory: attach active user with a default strategy
  _ = a.AttachUser("user_42", agent.DefaultUserStrategy())

  // Task root with `task/` inputs and empty `progress/`
  taskRoot := "/path/to/task-root"
  final, err := a.DoTask(ctx, taskRoot, agent.TaskSpec{
    Goal: "Summarize latest AI governance proposals and draft a 1-page brief",
    Budgets: agent.Budgets{ MaxWallClock: 3 * time.Minute, MaxSteps: 10, StepToolTimeout: 60 * time.Second },
  })
  if err != nil { log.Fatal(err) }

  log.Println("Progress dir:", final.ProgressDir)
}
```

## Example: Task/Progress Conventions

- `task/brief.md` — initial problem description and acceptance criteria.
- `task/links.txt` — seed URLs.
- On Gather, the Agent writes to:
  - `progress/findings.md` — high‑signal facts extracted from `task/` and the web.
  - `progress/artifacts/` — raw fetch results.
  - `progress/sources.jsonl` — citations with URL/title/snippet.
- On Plan, the Agent writes:
  - `progress/plan/current.json` — structured plan; if approvals required, also creates `progress/approvals/requests/plan-<id>.json`.
- On Act, the Agent writes:
  - `progress/events.ndjson` — tool call summaries; errors include retries and durations.
  - `progress/artifacts/` — generated content.
- On Verify, the Agent writes:
  - `progress/verify/0001-report.json` — pass/fail with reasons and pointers to artifacts.

## Interop With Existing Code

- Runner: multi‑step execution with tools, stop conditions, and finalizers (see `apps/webdemo/backend/handlers.go:248` and `apps/webdemo/backend/streaming.go:36`).
- Skills + sandbox: versioned, isolated execution (see `examples/skills/sandbox_agent/main.go:138`).
- Observability: normalized completions and tool call records (see `apps/webdemo/backend/streaming.go:144`).

## Deployment Considerations

- Local dev: file‑based approvals and journals; stdout exporter for OTLP (`GAI_OBS_EXPORTER=stdout`).
- Staging/Prod: networked approvals (Slack/HTTP) can replace file broker later; enable OTLP and Braintrust sinks.
- Secrets: supply provider keys and Zep credentials via env; ensure redaction is enabled for logs/events.

## Roadmap And Open Questions

- Dynamic skill creation: allow the agent to propose a skill spec and route through approvals.
- Networked approvals: Slack or HTTP broker feeding into the same on‑disk decision files for consistency.
- Memory evolution: advanced dedupe/repair pipelines, provider‑defined TTL/tag policies, cross‑agent sharing.
- Verify strategies: richer acceptance criteria inference; plug‑in verifiers for code/data/web tasks.
- Budget heuristics: adaptive allocation to reserve Verify share and cap Gather’s exploration.
- Multi‑agent patterns: out of initial scope, but the journaling and approvals model should compose.

---

This design keeps the Agent small and explicit, while leveraging the mature pieces already in the repo: provider adapters, runner, skills/sandbox, and observability. The defaults are safe and auditable, with clear extension points for teams to tailor behavior without forking core logic.

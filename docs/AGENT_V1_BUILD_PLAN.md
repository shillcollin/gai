# GAI Agent V1 — Build Plan (Exhaustive)

This plan specifies the end‑to‑end implementation of the GAI Agent v1 as a general‑purpose, governance‑aware AI worker. It is designed for multi‑session continuity: each section stands alone with enough context to resume work later.

## Executive Summary

- Goal: Implement a provider‑agnostic Agent that executes a single task safely and reproducibly under budgets and approvals, with deterministic on‑disk journaling and resumability.
- Scope (v1): default Gather → Plan → Act → Verify loop; file‑based approvals; typed PlanV1 + signature and enforcement; budget types and partial enforcement; versioned Agent events; minimal memory injection; skill/sandbox integration; HTTP + CLI ergonomics; tests and docs.
- Non‑goals (v1): complex workflow graphs, multi‑agent negotiation, advanced memory pipelines, deep browser automation. These sit “outside” the worker.

## Guiding Principles

- Provider agnostic; lean on existing SDK (core/providers/runner/tools).
- Governance first: approvals, plan signatures, policy for tools, budgets, and auditable artifacts.
- Deterministic: on‑disk `task/` inputs and `progress/` outputs; versioned events; resumable state.
- DX: typed, minimal, predictable APIs; sensible defaults; clear logs and events.

## Repository Map (Current)

- Providers/Runner/Tools: `core/`, `providers/`, `runner/`, `tools/` (mature foundation for execution + tools).
- Sandbox/Skills: `sandbox/`, `skills/` (session + manifest + prewarm). Existing `agent/` contains bundle helpers.
- Observability: `obs/` (OTEL, usage); `stream/` helpers; `prompts/` system.
- Docs: `docs/DEVELOPER_GUIDE.md`, `docs/AGENT.md`, `docs/AGENT_EXAMPLE.md` (design + examples).

We will introduce a new package `agentx/` for the runtime (to avoid colliding with the existing `agent/` bundle helpers).

## Core Contracts To Freeze (v1)

- PlanV1 + signature (content‑hash): `version:"plan.v1"`, `goal`, `steps[]`, `allowed_tools[]`, `acceptance[]`. Signature is sha256 over RFC 8785 JCS‑canonical JSON. Enforced during Act.
- Approvals: pluggable `ApprovalBroker`; file‑based default writing requests/decisions under `progress/approvals/`; requests include `plan_sig`.
- Agent Events v1: `gai.agent.events.v1` NDJSON frames; mirror to obs; include `usage_delta` and `budgets_remaining` where applicable.
- State: `agent.state.v1` in `progress/state.json`; contains `phase`, `plan_sig`, `last_step_id`, `budgets`, cumulative `usage`, timestamps.
- Budgets + StopReasons: types include MaxWallClock/Steps/ConsecutiveToolSteps now; tokens/cost added but enforcement can be deferred.

## Task Folder Conventions

- `task/` (read‑only to agent): inputs (prompts, files, prior history jsonl/md).
- `progress/` (agent R/W): `notes.md`, `todo.md`, `state.json`, `plan/current.json`, `approvals/{requests,decisions}`, `events.ndjson`, `findings.md`, `verify/*`, `artifacts/*`, `steps/<id>/*`.

## Package Layout (New)

```
agentx/
  agent.go               # Agent interface + New()
  options.go             # AgentOptions, TaskSpec, ApprovalPolicy, Budgets, ResultView
  result.go              # DoTaskResult, DeliverableRef, StepReport
  loop_default.go        # Gather → Plan → Act → Verify implementation
  loop_helpers.go        # message assembly, tool filtering, step scaffolding
  plan/
    plan.go              # PlanV1, validator, AllowedTools enforcement helpers
    jcs.go               # JCS canonicalization + signature helpers
  approvals/
    broker.go            # ApprovalBroker interface + types
    file_broker.go       # file-based broker (requests/decisions)
  events/
    events.go            # AgentEventV1 type + writer (NDJSON)
    emitter.go           # helpers to emit v1 frames + obs mirroring
  state/
    state.go             # agent.state.v1 struct + read/write (atomic) + resume
  memory/
    manager.go           # BuildPinned/BuildPhase stubs + strategy types
    zep_adapter.go       # placeholder adapter wiring
  httpapi/
    server.go            # minimal handlers: doTask, doTaskAsync, events, result, resume
    ndjson.go            # NDJSON streaming helper
  cli/
    main.go              # gai-agent CLI: init/run/tail/resume (v1 minimal)
  roles/
    registry.go          # Role presets (persona + allowed tools + memory bindings)
```

Notes:
- Keep public API small under `agentx` root; subpackages are internal helpers but can be imported if needed.

## Public Go API (Minimal)

```go
// Agent construction
func New(opts AgentOptions, r *runner.Runner, mm memory.Manager, ab approvals.ApprovalBroker) Agent

// Synchronous single-task execution
func (a Agent) DoTask(ctx context.Context, root string, spec TaskSpec, opts ...DoTaskOption) (DoTaskResult, error)

// Lower-level control (optional in v1)
StartTask / Run / StepOnce / Stop / Resume
```

Key types (from docs/AGENT.md): `AgentOptions`, `TaskSpec`, `Budgets`, `ApprovalPolicy`, `DoTaskResult`, `StepReport`, `ResultView`.

## HTTP Surface (for future TS SDK)

- POST `/agent/tasks/do` → `DoTaskResult` once finished (long‑poll OK in v1).
- POST `/agent/tasks/do/async` → `{ task_id }`.
- GET `/agent/tasks/{id}/events` → NDJSON stream of `AgentEventV1`.
- GET `/agent/tasks/{id}/result` → `DoTaskResult`.
- POST `/agent/tasks/{id}/resume` → `{ task_id }`.
- POST `/agent/approvals/{id}/decision` → write decision (HTTP broker; optional v1).

## Default Loop (v1)

- Gather
  - Inputs: persona, seed history, `task/` files, memory pinned + phase recall.
  - Tools: read‑only (e.g., `http_fetch`, `repo_search`, `file_read`).
  - Outputs: `progress/findings.md`, `sources.jsonl` (optional), notes update.
  - Events: `phase.start/finish`, tool.call/result.

- Plan
  - Compose PlanV1 from Gather outputs and inputs.
  - Persist to `progress/plan/current.json` and compute `plan_sig`.
  - If policy requires: emit `approval.requested`; await decision via broker; on deny/expire → StopReason, finish.

- Act
  - Enforce `AllowedTools`: wrap runner ToolHandle set with a gate that rejects tools not in the plan.
  - Execute with `runner.ExecuteRequest`, stop when `NoMoreTools` or budgets trigger.
  - Persist step events to `progress/events.ndjson` as AgentEventV1.

- Verify
  - Generate verification summary; write `progress/verify/0001-report.json`.
  - Optionally parse acceptance criteria; minimal v1 can be textual.

- Finalize
  - Update `state.json` with finish reason, usage, timestamps.
  - Emit `finish` event (with reason + budgets remaining).

## Approvals (File Broker v1)

- Types from docs/AGENT.md: `ApprovalKind`, `ApprovalRequest{ id, type, plan_path, plan_sig, ... }`, `ApprovalDecision{ id, decision, approved_by, timestamp }`.
- File broker behavior:
  - Submit: create `progress/approvals/requests/<id>.json`; idempotent.
  - Await: poll `decisions/<id>.json`; expire if past `expires_at`.
  - Stale plan check: if `type==plan`, require `plan_sig` to match; otherwise reject as stale.
  - Events: `approval.requested` and `approval.decided` with decision payload.

## Events (gai.agent.events.v1)

- Frame: `{ version, id, type, ts, step?, step_kind?, message?, data?, error? }`.
- Write NDJSON to `progress/events.ndjson`; mirror to obs.
- Canonical types in v1: `phase.start`, `phase.finish`, `approval.requested`, `approval.decided`, `tool.call`, `tool.result`, `usage.delta`, `budget.hit`, `finish`, `error`.
- Include `usage_delta` per step and `budgets_remaining` after budget‑affecting operations.

## PlanV1 & Signature

- Structure: `{ version:"plan.v1", goal, steps[], allowed_tools[], acceptance[] }`.
- Signature: sha256 over RFC 8785 JCS canonical JSON.
- Enforcement: gate tool calls in Act; re‑plan + new approval required to extend tools list (if policy requires).

## State (agent.state.v1)

- Minimal fields: `phase`, `plan_sig`, `last_step_id`, `budgets`, cumulative `usage`, `started_at`, `updated_at`.
- Atomic writes: write to `*.tmp`, then rename.
- Resume: read state, reopen events stream, continue from phase boundary; runner state is reconstructed from persisted artifacts where necessary.

## Budgets & Stop Reasons

- Budgets (v1 enforcement): MaxWallClock, MaxSteps, MaxConsecutiveToolSteps.
- Budgets (defined; enforce later): MaxTokens, MaxCostUSD.
- StopReasons: include `budget.max_*`, `approval.denied|expired`, `no_more_tools`, `completed`, `error`.

## Tools & Enforcement

- Default included tools (examples; can live in calling app or a `toolsx` subpackage):
  - `http_fetch` (GET only; redact headers in logs; domain allowlist optional).
  - `fs_read` (scoped to `task/` and `progress/`).
  - `fs_write` (scoped to `progress/`; approval‑gated by policy).
  - `sandbox_exec` (default read‑only; approval required for write/network; powered by `sandbox.Manager`).
- Enforcement layer wraps ToolHandles to:
  - Check `AllowedTools`.
  - Scope file operations to `progress/` (for write) and `task/|progress/` (for read).
  - Attach sandbox session (when skill is active) into `ToolMeta.Metadata["sandbox.session"]`.

## Memory (Minimal v1)

- Provide `memory.Manager` with:
  - `BuildPinned(ctx)` and `BuildPhase(ctx, phase, taskCtx)` that return small `[]core.Message`.
  - Strategy types: pinned enabled/max tokens; per‑phase queries; dedupe flags.
- Zep adapter stubbed behind interface; real retrieval optional in v1.
- Persist compiled memory blocks under `progress/` (optional), for reproducibility.

## Observability

- Mirror AgentEvents into `obs` using existing helpers.
- Record model usage totals from runner results; compute `usage_delta` per step for events.
- Emit metrics counters for `agent.phase`, `agent.approval`, `agent.finish_reason`.

## Security & Policy

- Redaction: scrub credentials in events and logs; sensitive tool args redacted.
- Filesystem: all writes restricted to `progress/` by default; rejects outside paths.
- Network: `http_fetch` GET default; POST requires approval (policy knob) and allowlist.

## CLI (v1 minimal)

- `gai-agent init <task-root>` → scaffold `task/` and `progress/` with starter files.
- `gai-agent run <task-root> [--role <id>]` → run DoTask.
- `gai-agent tail <task-root>` → tail `progress/events.ndjson`.
- `gai-agent resume <task-root>` → resume from `state.json`.

## Test Strategy

- Unit tests
  - Plan: signature stability across key orders; enforcement of `AllowedTools`.
  - Approvals: file broker idempotency, expiry, stale plan.
  - State: atomic write/read; resume boundary.
  - Events: v1 frame validity; NDJSON append.
- Integration tests
  - Loop end‑to‑end with fake tools and provider; approvals required/denied; budget hits; resume mid‑Act.
  - Sandbox smoke with `NewSandboxCommand` (no network).
- Contract tests
  - HTTP endpoints (do/doAsync/events/result) with NDJSON streaming; idempotency key.

## Performance Considerations

- NDJSON buffered writer; flush after important events.
- Avoid excessive prompt growth: seed history policy defaults to small LastN.
- Use runner parallelism only when model requests multiple tools in one step.

## Rollout Plan (Milestones)

- M0 — Scaffolding & Contracts
  - Create `agentx/` package skeleton and types: `AgentOptions`, `TaskSpec`, `Budgets`, `ApprovalPolicy`, `DoTaskResult`, `StepReport`, `ResultView`.
  - Implement `events.AgentEventV1` + NDJSON writer.
  - Implement `plan.PlanV1` + JCS canonicalizer + `Signature()`.
  - Implement `state.agentStateV1` + atomic read/write helpers.
  - Deliverables: compilable package, unit tests for plan/events/state.
  - Acceptance: plan signature deterministic; event frames valid; state read/write round‑trip.

- M1 — Approvals & Loop Skeleton
  - `approvals.FileBroker` with Submit/Await/GetDecision; on‑disk schema w/ `plan_sig`.
  - Default loop functions (scaffold only), events emission, and phase boundaries updating `state.json`.
  - Minimal tools registry and enforcement stub for `AllowedTools`.
  - Acceptance: Plan approval path blocks/continues; denied/expired returns StopReason.

- M2 — Act Phase & Runner Integration
  - Tool enforcement wrapper; allowed tools gate; map tool calls to events.
  - Execute with `runner.ExecuteRequest`; stop conditions from budgets; per‑step `usage_delta` events.
  - Verify phase and report writeout.
  - Acceptance: Gather→Plan→Act→Verify completes on a synthetic task; events written; state finalized.

- M3 — CLI & HTTP
  - Minimal HTTP endpoints for do/doAsync/events/result; NDJSON streaming.
  - CLI commands run/tail/resume.
  - Acceptance: end‑to‑end via HTTP and CLI; replayable via `progress/`.

- M4 — Hardening
  - Docs polish; security review; test flakiness; corner cases (resume after crash, partial files, plan change mid‑run).
  - Optional: add Slack/HTTP approval broker built on same interface.

## Detailed Task Breakdown (Checklists)


### Types & Contracts
- [x] `agentx/options.go`: AgentOptions, TaskSpec, ApprovalPolicy, Budgets, ResultView, Runner options, broker factory.
- [x] `agentx/result.go`: DoTaskResult, DeliverableRef, StepReport.
- [x] `agentx/events/events.go`: AgentEventV1 + constants + validator.
- [x] `agentx/plan/plan.go`: PlanV1, Validate(), AllowsTool, Signature().
- [x] (folded into plan.go) canonical JSON via deterministic `encoding/json` ordering.
- [x] `agentx/state/state.go`: StateV1 struct + AtomicWrite/Read + tests.

### Approvals
- [x] `agentx/approvals/broker.go`: interface + types (ApprovalKind/Request/Decision).
- [x] `agentx/approvals/file_broker.go`: submit/await/get with expiry and stale plan protection; tests.

### Loop
- [x] `agentx/agent.go`: agent lifecycle, Gather → Plan → Act → Verify with state/events/approvals.
- [x] Loop helpers integrated into `agentx/agent.go` (message assembly, approvals, deliverables, usage accounting).
- [ ] TODO(loop): reintroduce a pluggable LoopFunc so users can compose custom phase graphs.

### Memory
- [x] `agentx/memory/manager.go`: Strategy structs + BuildPinned/BuildPhase implementation with tests.
- [ ] `agentx/memory/zep_adapter.go`: placeholder wiring (TODO).

### Tools & Sandbox
- [x] Enforcement wrapper via `approvalToolHandle` enforcing allowed tools & approvals.
- [x] Sandbox session integration via `AgentOptions.SandboxManager` + per-skill assets.

### Observability
- [x] Usage delta events now include budgets remaining; finish/error events emitted; additional metrics via `obs` optional later.

### HTTP & CLI
- [x] `agentx/httpapi/server.go`: synchronous/async task endpoints, results, NDJSON streaming.
- [ ] Dedicated NDJSON helper (stream logic embedded in handler; optional extraction).
- [x] `cmd/gai-agent/main.go`: CLI with run/tail/resume commands hitting HTTP API.

### Tests
- [x] Unit: plan/events/state/approvals, agent tool gating, memory injection.
- [x] Integration-lite: HTTP server exercising loop via stub provider + approvals; resume/budget scenarios covered in agent tests.

## Risks & Mitigations

- Plan/Act drift: enforce AllowedTools; re‑plan path emits new `plan_sig` and requires new approval (if configured).
- Prompt bloat: default small LastN/SeedOnly; memory compiler later.
- Partial writes: atomic write helpers for all progress files; retry on rename when necessary.
- Sandbox availability: feature flag skills/sandbox; degrade gracefully when not configured.

## Documentation To Ship With v1

- Agent README: quickstart, assumptions, examples (research, bugfix), approvals flow, budgets.
- Event schema doc: `gai.agent.events.v1` types + examples; how to stream.
- Plan doc: `PlanV1` structure, signature, and enforcement.
- State doc: `agent.state.v1` with resume behavior.
- CLI and HTTP reference.

---

This plan is aligned with the updated design in `docs/AGENT.md` and grounded in existing packages. It locks the seams that matter (broker, events, plan/signature, budgets/state) while keeping the worker focused and composable by external orchestrators.

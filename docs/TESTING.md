# Testing Strategy

Goal: high signal, honest tests that detect regressions without forcing us to fake model behavior or hard‑code brittle outputs.

Principles
- Assert structure and invariants, not exact generations.
- Separate fast unit/contract tests from opt‑in live provider tests.
- Keep live tests budgeted and gated by env vars.
- Prefer property/fuzz tests for parsers, validators, and repair logic.

Test Taxonomy
- Unit
  - Serialization of messages/parts → provider requests.
  - Prompt registry rendering, overrides, helpers, fingerprints.
  - Structured outputs: schema derivation, repair paths, edge values.
  - Stop conditions combinators; `FinalState` stop reasons; deterministic tool result ordering.
  - Stream packers: normalized event construction and ordering.
- Contract (no network)
  - Validate sample streams against `gai.events.v1` schema using fixtures in `docs/schema/examples`.
  - Enforce invariants: strictly increasing `seq`, exactly one terminal event, tool call/result correlation.
  - Reasoning policy gating logic (`start.policies.reasoning_visibility`).
  - Obs hooks: use OTel stdout exporters; assert span attributes exist.
- Integration (live providers; opt‑in)
  - Gated by env vars; skipped when unset. Environment matrix:
    - OPENAI_API_KEY, ANTHROPIC_API_KEY, GOOGLE_API_KEY, ELEVENLABS_API_KEY, CARTESIA_API_KEY, GROQ_API_KEY...
    - GAI_LIVE=1 to enable the suite.
  - Assertions
    - Non‑empty text, valid finish reason, structured outputs parse (or repair then parse).
    - Stream events pass schema validation; reasoning summary respected by default policy.
    - Tool call/result round‑trip via simple demo tools.
    - Provider‑specific features (e.g., Gemini citations) present structurally when enabled.
  - Guardrails
    - MaxTokens ≤ 256 by default; per‑suite max request budget; backoff sleeps; per‑provider RPS caps.
- E2E
  - Start a demo SSE server, run a client that consumes events and validates with `cmd/gai-events-validate`.
  - Optional UI snapshot of event rendering using TS types (no DOM snapshots).
- Performance/Concurrency
  - Benchmarks for stream normalization, token estimation, tool concurrency queue.
  - Run `-race` for runner/tool tests.

Running Tests
- Unit/Contract
  - go test ./... (defaults; no live calls)
  - Stream validation: `go run ./cmd/gai-events-validate --file docs/schema/examples/basic_text.ndjson`
- Integration (live)
  - export GAI_LIVE=1 and relevant keys; then `go test -tags live ./examples/...` or run example binaries.
- Budget/Cost
  - Examples accept `--max-tokens`, `--budget-ms`, and `--rps` flags to constrain spend.

What We Don’t Do
- No hard‑coded target text; no “simplify source to pass tests”.
- No opaque mocks for core behavior; use fakes only at the provider boundary for unit tests.

Repository Layout & Conventions
- Place unit tests next to source files; contract tests under `internal/contracttests`.
- Live tests live under `examples/` and may include `_test.go` wrappers with `//go:build live`.
- Name tests with clear intent: `TestStream_Normalized_Ordering`, `TestRunner_StopReason_MaxSteps`.

Property/Fuzz Testing Examples
```go
func FuzzRepairJSON(f *testing.F) {
  f.Add(`{"name":"x","items":[1,2,3]}`)
  f.Fuzz(func(t *testing.T, s string) {
    out, err := core.RepairJSON([]byte(s))
    if err == nil {
      var any map[string]any
      if json.Unmarshal(out, &any) != nil {
        t.Fatalf("repaired JSON must unmarshal")
      }
    }
  })
}
```

Tool/Runner Determinism Test (unit)
```go
func TestRunner_DeterministicAppend(t *testing.T) {
  // create 3 tools that return in random order; assert appended order stable
}
```

CI Tips
- Run `-race` on runner/tools package tests.
- Cache module downloads; skip live suites unless `GAI_LIVE=1` and keys present.

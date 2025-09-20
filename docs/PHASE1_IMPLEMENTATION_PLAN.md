# Phase 1 Implementation Plan — gai Go SDK

_Last updated: 2025-09-20_

## 1. Phase Goals
- Deliver a production-quality foundation that matches the Phase 1 scope in `docs/ROADMAP.md`.
- Provide a stable, typed provider-agnostic API (`core`), normalized streaming, structured outputs, typed tools, the runner, prompt registry, observability hooks, and developer-facing examples/tests.
- Ship a publicly consumable Go module (`go.mod`) that external teams can try end-to-end with OpenAI, Anthropic, Gemini, and OpenAI-compatible providers.

## 2. Guiding Principles
1. **Go-first ergonomics** – idiomatic APIs, clear packages, no reflection in hot paths.
2. **Provider agnostic** – all features must work across adapters with consistent behavior and error taxonomy.
3. **Observable by default** – traces, metrics, and prompt IDs flow through every request.
4. **Testing discipline** – unit + contract tests required; live tests opt-in via env flags.
5. **Docs-driven development** – keep the Developer Guide, API Reference, and examples in lockstep with code.

## 3. Timeline & Milestones (Indicative)
- **Week 1:** Repository bootstrap (go.mod, lint/test tooling), `core` package, streaming scaffolding, internal utilities.
- **Week 2:** Provider adapters (OpenAI + Anthropic), HTTP client stack, error taxonomy wired, contract tests for streaming fixtures.
- **Week 3:** Gemini adapter, compat adapter, file/blob handling, prompt registry MVP.
- **Week 4:** Structured outputs (strict + repair), typed tools, runner orchestration, stop conditions, finalizers.
- **Week 5:** Observability package, OpenTelemetry instrumentation, metrics, cost tracking, `cmd/gai-events-validate` CLI.
- **Week 6:** Examples, documentation updates, test hardening (race detector, fuzz), release candidate cut + changelog.

(Adjust timeline based on team bandwidth; milestones mark integration points.)

## 4. Workstream Breakdown

### 4.1 Repository Bootstrap & Tooling
- Initialize `go.mod` (module `github.com/shillcollin/gai`), set Go 1.22 minimum.
- Add `go.sum` via `go mod tidy` after first packages land.
- Configure linting (`golangci-lint` config), formatting (goimports), and Makefile or `mage` tasks for `fmt`, `lint`, `test`, `integration`.
- Set up CI workflow (GitHub Actions) with jobs: lint, unit tests, race tests, (optional) coverage upload.
- Add `.editorconfig`, `.golangci.yml`, `.gitignore` for Go + examples + build outputs.

**Definition of Done (DoD):** running `make test` (or equivalent) passes on clean checkout; CI green.

### 4.2 Core Package (`core/`)
- Implement foundational types from `docs/API_REFERENCE.md` (Request, Message/Part hierarchy, Provider interface, Usage, StopReason, Safety types, etc.).
- Provide helper constructors (SystemMessage, UserMessage, TextPart, BlobRef constructors, SimpleRequest, ChatRequest).
- Implement token estimation stubs (actual estimation delegated to `internal/tokens`).
- Codify error taxonomy (`AIError`, codes, helper predicates) and expose friendly wrappers.
- Define structured output wrappers (`ObjectResult[T]`, typed streaming wrappers) referencing `internal/jsonschema` for validation.
- Add cost estimation helper skeleton; wire pricing maps later in providers.

**Tests:**
- Unit tests for message serialization, helper builders, BlobRef validation, error helpers.
- Property/fuzz tests for JSON repair entry points (once implemented).

### 4.3 Internal Utilities (`internal/`)
- `internal/httpclient`: construct shared `http.Client` with tuned transport, timeouts, retries; support override via Provider options.
- `internal/jsonschema`: integrate `github.com/santhosh-tekuri/jsonschema/v5` (or similar) for validation; add schema generation hooks for tools/structured outputs; implement repair pipeline (likely `jsonrepair` or in-house algorithm).
- `internal/observability`: common span attribute helpers, cost/token recording, instrumentation wrappers.
- `internal/stream`: event normalization helpers, sequence validation, NDJSON/SSE encoders/decoders aligned with schema.
- `internal/tokens`: approximate token estimators per provider; allow pluggable overrides when official models available.

**Tests:**
- Targeted unit tests + fuzz (e.g., schema repair). Contract tests using fixtures under `docs/schema/examples`.

### 4.4 Streaming (`stream/`)
- SSE and NDJSON writers that accept `core.StreamEvent` and flush appropriately.
- Reader abstraction that can consume SSE or NDJSON streams and emit typed events.
- Backpressure support (configurable buffer, error propagation).
- Validation hook to enforce `gai.events.v1` schema (delegate to `internal/stream`).

**Tests:** contract tests for event ordering, SSE/NDJSON parity, schema validation using fixtures.

### 4.5 Provider Adapters (`providers/`)

#### OpenAI (`providers/openai`)
- Implement API client covering `/chat/completions` (text + streaming), `/responses` (structured outputs once available), file uploads if needed for multimodal.
- Provide typed option builders (`WithModel`, `WithJSONMode`, `WithSeed`, penalties, logprobs, etc.).
- Map OpenAI errors to `core.AIError` taxonomy, including rate limits, content filter, etc.
- Support parallel tool calls if provider allows; convert tool call formats to `core`.

#### Anthropic (`providers/anthropic`)
- Support Claude messages API with thinking tokens, safety settings, tool use.
- Map streaming format to normalized events, handling `thinking`/`reasoning` tokens.
- Option builders (`WithThinkingEnabled`, `WithMaxThinkingTokens`, `WithCacheControl`).

#### Gemini (`providers/gemini`)
- Integrate `google/generative-ai-go` or HTTP client; support multimodal inputs, citations, safety reasons.
- Handle file uploads (BlobRef path/url support) and streaming responses.

#### Compat (`providers/compat`)
- Generic adapter for OpenAI-compatible APIs: parameterized base URL, headers, JSON schema toggles.
- Provide minimal typed options for base URL, organization, compatibility flags (strict JSON, streaming differences).

**Common Tasks:**
- Shared request builder translating `core.Request` to provider-specific payloads.
- Streaming normalization: convert provider event stream to `core.StreamEvent` using `internal/stream`.
- Capability detection (capabilities struct, available models listing).
- Pricing tables + usage conversion for cost estimation.

**Tests:**
- Unit tests using stub HTTP servers with canned responses (use `httptest`).
- Contract tests ensuring normalized streams validate against schema.
- (Optional) integration tests behind `GAI_LIVE=1` requiring real provider keys.

### 4.6 Structured Outputs (`core` + `internal/jsonschema`)
- Implement strict mode pipeline: prefer provider strict JSON (OpenAI, Anthropic, Gemini) when available.
- Implement repair fallback using JSON repair algorithm when provider output invalid.
- Provide typed wrappers (`GenerateObjectTyped`, `StreamObjectTyped`) with generics.
- Expose API to attach explicit schema or rely on derived schema from Go structs.
- Add metrics for repair rate, validation failures.

**Tests:** derive schema from sample structs, validate outputs, ensure repair handles fuzzed malformed JSON.

### 4.7 Tools & Runner (`tools/`, `runner/`)
- Typed tool definitions with schema derivation via reflection tags; enforce description, enum/min/max tags.
- Implement `tools.CoreAdapter` bridging typed tools to `core.ToolHandle` interface.
- Runner orchestrates steps: request execution, tool dispatch (parallelism), stop conditions (MaxSteps, NoMoreTools, custom combinators), finalizers (`OnStop`).
- Support metadata propagation (call IDs, trace IDs) to tools, integrate memoization and retry policies.
- Provide built-in finalizers (summary, validation) and stop condition helpers.

**Tests:**
- Unit tests verifying deterministic tool ordering, stop conditions, error handling modes.
- Mock provider to simulate tool call suggestions and streaming interplay.

### 4.8 Prompts Registry (`prompts/`)
- Implement registry loading from embedded FS, override dir, helper registration, fingerprinting (SHA256 of template text).
- Provide API to list versions, detect overrides, reload.
- Expose telemetry hooks to attach prompt ID to spans/events.

**Tests:** unit tests for template rendering, overrides, helper functions, fingerprint stability.

### 4.9 Observability (`obs/`) & Cost Tracking
- Initialize OpenTelemetry exporter (stdout in dev, OTLP config), return shutdown function.
- Provide helper to instrument providers (wrapper that records spans, metrics, errors).
- Emit metrics: request count/latency, tokens, cost, tool counts, stream events.
- Support hooking custom metric recorder interface when OTEL not desired.

**Tests:** use OTEL test exporter to assert attributes; ensure prompt IDs recorded.

### 4.10 CLI Utilities (`cmd/gai-events-validate`)
- Implement CLI that reads NDJSON/SSE, validates against schema using `internal/stream` + `docs/schema/gai.events.v1.json`.
- Flags: `--file`, `--stdin`, `--format` override, `--strict` (fail on warnings), `--summary`.
- Provide exit codes (0 success, 1 validation failure, >1 program error).

**Tests:** CLI integration tests with fixtures from `docs/schema/examples`.

### 4.11 Examples (`examples/`)
- **basic:** simple text generation across providers, conversation helper.
- **streaming:** streaming to stdout, backpressure example, reasoning visibility toggles.
- **structured:** typed JSON generation, validation error handling.
- **tools:** tool registration + runner multi-step scenario.
- **observability:** example that wires OTEL exporter and logs metrics.

Examples should compile and run with placeholder API keys; guard actual execution behind environment variables.

### 4.12 Documentation & Changelog
- Update `docs/DEVELOPER_GUIDE.md` with real code snippets excerpted from examples (use literate testing where possible).
- Expand `docs/API_REFERENCE.md` via go doc extraction or templated generation.
- Create `CHANGELOG.md` summarizing Phase 1 outputs.
- Ensure README quick start references actual packages and commands.

## 5. Cross-Cutting Quality Gates
- **Testing:** `go test ./...` mandatory, include contract tests; add `go test -race ./...` in CI (nightly if slow).
- **Fuzzing:** add fuzz harness for JSON repair and stream validation (run locally/nightly).
- **Static Analysis:** golangci-lint enabling `errcheck`, `gosimple`, `staticcheck`, `govet`, `revive`.
- **Security:** ensure HTTP clients honor TLS defaults, secrets via env vars only, avoid logging PII.
- **Performance:** microbenchmarks for stream normalization, token estimation, prompt registry; track regressions via `go test -bench`.

## 6. Dependencies & External Requirements
- Provider SDKs/HTTP endpoints (OpenAI, Anthropic, Google Gemini); ensure env var layout documented (`OPENAI_API_KEY`, etc.).
- OpenTelemetry libraries (`go.opentelemetry.io/otel`, exporters).
- JSON schema/repair libraries (evaluate existing libs vs in-house implementation).
- Token counting (tiktoken or custom approximations) – assess license compatibility.

## 7. Deliverables Summary (Phase 1 Exit Criteria)
1. Public Go module with version tag `v0.1.0` (or similar) ready for early adopters.
2. Working adapters for OpenAI, Anthropic, Gemini, and configurable OpenAI-compatible endpoints.
3. Normalized streaming API with validators + CLI.
4. Typed tools + runner enabling multi-step execution with stop conditions.
5. Structured output support with validation and repair pipeline.
6. Prompt registry with overrides and fingerprinting.
7. Observability instrumentation (traces, metrics, cost) + example dashboards guidance.
8. Comprehensive documentation and runnable examples.
9. Automated tests (unit, contract, optional live) with CI coverage.

## 8. Risks & Mitigations
- **Provider API churn:** encapsulate provider-specific payloads; expose compatibility flags; add integration tests with feature detection.
- **Token counting accuracy:** start with approximations; capture actual usage from responses; adjust estimators in follow-up releases.
- **Cost overruns in tests:** enforce small `MaxTokens`, add rate limiting middleware in examples, require explicit env flag for live runs.
- **Streaming differences:** maintain fixture suite per provider; use CLI validator in CI to catch regressions.
- **Concurrency bugs in runner/tools:** rely on race detector in CI; add deterministic tests with seeded randomization.

## 9. Open Questions (Track & Resolve Early)
- Final license choice (MIT vs Apache-2.0) – required before public release.
- Preferred JSON schema library (built-in vs third-party) considering size/performance.
- How to distribute token pricing tables (embed vs fetch) – needed for cost estimator.
- Long-term plan for provider capability discovery (static list vs API-based fetch).

## 10. Next Actions Checklist
1. Confirm Go version + tooling baseline with team.
2. Approve directory layout introduced in this plan (see repository root).
3. Kick off repository bootstrap tasks (go.mod, lint config, CI).
4. Start implementation with `core` package and internal utilities as foundations for other workstreams.
5. Schedule weekly Phase 1 sync to monitor milestones and unblock dependencies.

---

This document should be updated as deliverables evolve. Treat it as the living backlog for Phase 1 work.

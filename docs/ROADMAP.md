# gai Roadmap

Vision: Build the best production AI framework for Go — typed, observable, fast — with first‑class DX for providers, tools, structured outputs, streaming, prompts, and voice.

Pillars
- Developer Experience: Clear API shapes, minimal ceremony, great docs and examples.
- Performance: Concurrency, streaming, zero‑copy hot paths, sensible timeouts.
- Observability: OTel tracing/metrics, usage/cost accounting, easy telemetry.
- Interop: Normalized streams, OpenAI‑compat adapter, MCP import/export.
- Reliability: Retries, rate limits, stop conditions, finalizers, safety hooks.

Milestones
- Phase 1 (Publishable Foundation)
  - Providers: OpenAI, Anthropic, Gemini, OpenAI‑compatible adapter (e.g., Groq/xAI/Baseten/Cerebras).
  - Core: `core.Provider`, `core.Request`, messages & parts, structured outputs (strict + repair), typed tools & `Runner` with stop combinators, `OnStop` finalizers.
  - Streaming: Unified `gai.events.v1` schema (SSE/NDJSON), reasoning policy (none|summary|raw), fixtures + validator, UI SSE wrapper with toggles.
  - Prompts: Registry (semver, overrides, fingerprint), prompt IDs in spans.
  - Observability/Evals: OTel wiring, default spans/metrics, tiny eval harness + sink export (per `docs/PRD_OBS_EVALS.md`).
  - Files: `BlobRef` (URL/Bytes/ProviderFileID), Gemini upload path.
  - Docs & Examples: Developer guide, streaming spec, testing guide, provider docs; runnable examples with real providers.

- Phase 2 (Core Extensions)
  - Router: latency/cost/AB/failover policy, per‑request overrides.
  - Memory & local‑first RAG (sqlite‑vec), citation stitching helpers.
  - Voice: STT (Whisper/Deepgram) + TTS (ElevenLabs/Cartesia), streaming `audio.delta` to browsers.
  - Cloudflare AI Gateway middleware.
  - MCP client/server (tools/resources/prompts import/export), auth for HTTP server.
  - Expanded evals + dataset runners; richer sinks.

- Phase 3 (Provider Depth + Perf)
  - Additional providers & quirks documented.
  - Provider‑specific features surfaced via `ProviderOptions` (and typed wrappers later if needed).
  - Perf hardening: pool tuning, backpressure knobs, race/soak tests, benchmarks.

Publish Gates
- End‑to‑end examples run green with real providers.
- Streams validate against schema (fixtures + validator).
- Tool loops & structured outputs pass acceptance criteria (typed parse/repair, stop reasons).
- OTel emits spans/metrics and prompt IDs.

Success Metrics (initial targets)
- Developer adoption: 5+ external projects using gai within 1 month of P1.
- Stability: <0.1% error rate in examples over 1k runs; zero panics/races under fuzz.
- Performance: P50 time‑to‑first‑token < 600ms on fast models; overhead < 5% vs raw HTTP.
- Observability: 100% of requests produce usable spans/metrics; prompt IDs captured for 95% of calls.

Timeline (indicative)
- Weeks 1–3: Phase 1 core types, OpenAI + Anthropic adapters, streaming normalization + SSE/NDJSON, prompts, obs wiring.
- Weeks 4–5: Gemini adapter (files, safety, citations), structured outputs strict/repair, tools/runner + OnStop, examples.
- Week 6: Docs polish, tests (unit/contract/live), publish P1.
- Weeks 7–10: Phase 2 router, RAG helpers, Cloudflare gateway, voice primitives, MCP; examples + docs.
- Weeks 11–12: Phase 3 providers + perf + benchmarks.

Dependencies & Risks
- Provider API churn → Maintain adapters with feature flags and robust error mapping.
- Browser streaming variability → Stick to SSE/NDJSON; provide validator and fixtures; document CORS/headers.
- Cost unpredictability in tests → Enforce budgets/timeouts, small MaxTokens defaults, and env‑gated live suites.
 - Raw pass‑through safety → Keep `ext.raw` behind explicit server policy and avoid schema coupling to raw.

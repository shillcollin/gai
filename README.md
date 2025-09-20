gai — Go AI SDK (docs-first)

Status
- This repository currently contains documentation, schema, and examples for the upcoming Go SDK named `gai` (repo: github.com/shillcollin/gai).
- Code is not yet implemented; we are finalizing PRDs and the build plan before scaffolding packages.

Quick Links
- Developer Guide: `docs/DEVELOPER_GUIDE.md`
- Product Requirements (PRDs):
  - Core: `docs/PRD_CORE.md`
  - Runner & Tools: `docs/PRD_RUNNER_TOOLS.md`
  - Structured Outputs: `docs/PRD_STRUCTURED_OUTPUTS.md`
  - Streaming Normalization: `docs/PRD_STREAMING_NORMALIZATION.md`
  - Providers & Capabilities: `docs/PRD_PROVIDERS.md`
  - Observability & Evals: `docs/PRD_OBS_EVALS.md`
  - UI (Transcript & Hooks): `docs/PRD_UI.md`
  - Voice: `docs/PRD_VOICE.md`
- Streaming Spec: `docs/STREAMING_SPEC.md`
- UI Guide: `docs/UI_GUIDE.md`
- Schema + Types + Fixtures: `docs/schema/`
- Providers: `docs/PROVIDER_*`
- Testing & Evals: `docs/TESTING.md`, `docs/OBS_EVALS.md`
- Roadmap: `docs/ROADMAP.md`

Next Steps
- Decide on license (MIT or Apache-2.0 are common for SDKs).
- Finalize Phase 1 build plan (`docs/PHASE1_IMPLEMENTATION_PLAN.md`).
- Scaffold Go module and packages per `docs/PHASE1_IMPLEMENTATION_PLAN.md` once PRDs are stable.

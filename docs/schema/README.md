gai Normalized Stream Schema

- Schema: `docs/schema/gai.events.v1.json`
- Examples: `docs/schema/examples/*.ndjson`
- TypeScript types: `docs/schema/ts/gai.events.v1.d.ts`

Usage
- SSE: one JSON object per `data:` line
- NDJSON: one JSON object per line

Validate in development

```bash
go run ./cmd/gai-events-validate --file docs/schema/examples/basic_text.ndjson
```

Versioning
- The `schema` field is a hard contract key (e.g., `gai.events.v1`).
- Additive fields remain within v1; breaking changes bump to v2.
- The `start` event advertises optional capabilities and policies (e.g., reasoning visibility).

Reasoning events
- `reasoning.delta`: raw thinking tokens (policy gated, `policies.reasoning_visibility=raw`).
- `reasoning.summary`: concise, user‑safe rationale; may include provider signatures.

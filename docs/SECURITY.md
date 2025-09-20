# Security & Compliance Notes

Secrets
- Load API keys from environment variables; never commit.
- Prefer runtime secret stores for production; rotate keys regularly.

PII & Content
- Redact PII at stream time (middleware) when required.
- Avoid logging raw inputs/outputs unless necessary; use sampling.
- Record safety events and decisions for audit trails.

Data Retention
- Configure provider data retention settings where applicable.
- Keep prompt/content fingerprints for traceability without storing full text.

Network
- Use HTTPS; set reasonable timeouts; reuse `http.Client` with keep‑alives.
- Consider Cloudflare AI Gateway for rate limits and analytics.

Compliance
- Tag spans/metrics with tenant identifiers; avoid embedding PII in resource names.
- Maintain `usage` and `cost` metrics for billing/audit.

MCP
- For remote MCP servers, enforce auth (bearer tokens) if exposing HTTP.

Threat Model & Tools
- Validate tool inputs/outputs; treat tool execution boundaries as untrusted.
- For file processing, scan/validate file types; avoid executing untrusted content.

Logging & Audit
- Keep audit logs of tool calls (name, input hash, duration, error status).
- Avoid logging raw secrets or user content; use hashing/fingerprinting when possible.


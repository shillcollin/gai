# Performance Tuning

HTTP
- Reuse a single `http.Client` with tuned `Transport` (keep‑alives, idle pool size/timeouts).
- Enable gzip where providers support it; watch CPU trade‑offs.

TLS
- Reuse TLS sessions; avoid per‑request handshakes; consider `MaxIdleConnsPerHost`.

Streaming
- Prefer streaming for long outputs to reduce tail latency.
- Tune SSE/NDJSON buffer sizes; flush frequently on the server.
- Use backpressure (bounded channels) to prevent memory spikes.

Runner & Tools
- Set `WithMaxParallel` according to external API capacity; honor timeouts.
- Keep tool bodies small and idempotent; propagate errors or append and continue based on policy.

Structured Outputs
- Use strict JSON modes to save repair passes; validate and repair once if needed.

Memory & RAG
- Use sqlite‑vec batch inserts; keep embeddings small; pre‑truncate long context.

Go Runtime
- Profile with `-cpuprofile`/`-memprofile`; look for allocations in hot paths.
- Avoid reflection in core loops; reuse buffers where possible.

GOMAXPROCS & Scheduling
- Set `GOMAXPROCS` to match container CPU limits; avoid oversubscription.
- Use bounded goroutine pools for heavy tool concurrency.

Benchmarks
- Microbench: stream normalization, token estimation, prompt rendering.
- Macrobench: E2E stream throughput under tool concurrency.

Operational Tips
- Rate limit per provider to avoid cascaded failures; backoff with jitter.
- Circuit breakers (future) to trip on sustained provider errors.


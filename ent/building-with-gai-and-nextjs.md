# Building with GAI and Next.js

## What We Built
- Wired the ent demo's Go backend to stream responses from GAI providers, targeting Anthropic Claude models.
- Hooked the Next.js front end into the streaming API so UI updates land token-by-token alongside tool-call telemetry.
- Smoothed the provider configuration layer so the demo can swap between models while sharing common UX components.

## Day-to-Day Coding Experience
- **Go service layer**: GAI's provider abstraction made it straightforward to plug Anthropic in, but the transport layer is strict. The request builder mirrors core options, so any backend-only metadata leaks—like the step counter we tried—surface as API errors. Keeping a clean separation between orchestration state and provider payloads is essential.
- **Streaming ergonomics**: The `core.Stream` helper kept the backend readable. Consuming SSE from Claude felt reliable once we respected their event naming, and the Go channel pattern mapped cleanly onto Next.js's `ReadableStream` handling.
- **Next.js integration**: On the front end, Server Actions and dynamic routes made it easy to proxy streaming responses into UI components. Suspense boundaries helped isolate loading states while still showing partial model output.

## Insights & Learnings
1. **Metadata discipline**: Anthropic validates payloads aggressively. Passing extra fields like `metadata.step` breaks the request. Keep provider-specific metadata isolated and guard any experimental fields behind capability checks.
2. **Logging pays off**: The structured logs we added (`stream_request`, `stream_step_start`, etc.) made diagnosing the 400 easy. For multi-step orchestration, logging each transition with provider/model context is invaluable.
3. **Tooling parity matters**: Even when providers support parallel tool calls, confirm the capability flag before enabling it client-side. Falling back gracefully avoids mismatched expectations when swapping models.
4. **SSE in practice**: Scanner-based parsing works, but buffer sizes need tuning for large reasoning traces. We bumped the scanner buffer to 512 KiB to avoid truncated events—worth keeping in mind for verbose models.
5. **UX feedback loops**: The combination of streaming text and reasoning deltas keeps users engaged. Broadcasting those events to the Next.js UI required careful event typing, but once wired, the experience feels markedly better than chunked polling.

## What I'd Explore Next
- Add provider-specific metadata adapters so we can attach safe fields (e.g., `user_id`) without risking invalid keys.
- Capture latency and token usage metrics per step and surface them in the UI for better observability.
- Experiment with Next.js edge runtime to cut round-trip latency for streaming endpoints.

Overall, pairing GAI's abstractions with Next.js's streaming UI made the ent demo feel responsive and maintainable, as long as we respect each provider's contract and keep an eye on the logs.

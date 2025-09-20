# Prompt Management

Goals
- Versioned prompts with semver, fingerprinting for audit, runtime overrides, and helper functions.

Structure
```
prompts/
  summarize@1.0.0.tmpl
  summarize@1.2.0.tmpl
  tone_polite@0.1.0.tmpl
```

Usage
```go
//go:embed prompts/*.tmpl
var tmplFS embed.FS

reg := prompts.NewRegistry(
  tmplFS,
  prompts.WithOverrideDir(os.Getenv("PROMPTS_DIR")), // hot‑swap without rebuild
)
text, id, _ := reg.Render(ctx, "summarize", "1.2.0", map[string]any{"Audience":"exec","Length":"short"})

req := core.Request{ Messages: []core.Message{
  {Role: core.System, Parts: []core.Part{core.Text{Text: text}}},
  {Role: core.User, Parts: []core.Part{core.Text{Text: "Paste article..."}}},
}}
```

Helpers
- Built‑ins: indent, join, json/jsonIndent, default, upper/lower/title, now/date, first/last.
- Custom: `prompts.WithHelperFunc(name, fn)`; register stateless helpers.

Observability
- `id` includes name, version, fingerprint; attach to spans automatically when using the registry.

Ops Tips
- Keep prompts small and composable (partials).
- Derive guardrails (required bullets, word counts) in structured outputs rather than prompting only.

Partials & Includes
- Organize reusable fragments (e.g., “safety block”, “tone instructions”) as separate templates; include or render to strings and compose.

Testing Prompts
- Render prompts in unit tests and assert presence of critical markers (not full equality).
- Use eval runners to compare output quality across versions; tag runs with prompt IDs.

Versioning Strategy
- Bump PATCH for minor phrasing, MINOR for content changes, MAJOR for contract changes (e.g., switching output format).
- Maintain multiple active versions in source to allow controlled rollout via config.


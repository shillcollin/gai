# gai Documentation

**The Go AI SDK** — A Go-first, provider-agnostic SDK for building production AI applications with exceptional developer experience, broad functionality, and top-tier performance.

## Documentation Principles

### Core Philosophy
Every document in this repository serves a specific purpose: enabling developers to build production-ready AI applications using gai's Go-first, provider-agnostic API. We prioritize:

- **Clarity**: Clear, actionable documentation with real-world examples
- **Completeness**: Comprehensive coverage of all SDK features and capabilities
- **Consistency**: Uniform patterns and conventions across all providers
- **Go-first**: Native Go interfaces and types, with non-Go examples only for client consumption

### Source of Truth
- **Streaming Events**: `STREAMING.md` defines the authoritative `gai.events.v1` contract
- **API Surface**: `API_REFERENCE.md` defines all core types and interfaces
- **Provider Features**: `PROVIDERS.md` documents vendor-specific capabilities and mappings
- **Observability**: `OBSERVABILITY.md` defines metrics, traces, and evaluation standards

## Quick Navigation

### Getting Started
- **[DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md)** — Comprehensive guide with examples for all SDK features
- **[API_REFERENCE.md](API_REFERENCE.md)** — Complete API surface, types, and interfaces
- **[PROVIDERS.md](PROVIDERS.md)** — All provider adapters, setup, and best practices
- **[PROVIDER_MATRIX.md](PROVIDER_MATRIX.md)** — Quick feature comparison across providers

### Core Features
- **[STREAMING.md](STREAMING.md)** — Normalized event streaming (SSE/NDJSON) and `gai.events.v1` specification
- **[TOOLS_AND_RUNNER.md](TOOLS_AND_RUNNER.md)** — Typed tools, multi-step execution, and stop conditions
- **[STRUCTURED_OUTPUTS.md](STRUCTURED_OUTPUTS.md)** — Type-safe JSON generation with validation and repair
- **[PROMPTS.md](PROMPTS.md)** — Versioned prompt management with templates and overrides

### Production Features
- **[OBSERVABILITY.md](OBSERVABILITY.md)** — OpenTelemetry integration, metrics, and evaluation framework
- **[TESTING.md](TESTING.md)** — Testing strategy, patterns, and live provider tests
- **[SECURITY.md](SECURITY.md)** — Security best practices and compliance guidelines
- **[PERFORMANCE_TUNING.md](PERFORMANCE_TUNING.md)** — Optimization strategies and benchmarks

### Advanced Topics
- **[advanced/ROUTER.md](advanced/ROUTER.md)** — Model/provider selection with cost and latency policies
- **[advanced/VOICE.md](advanced/VOICE.md)** — STT → LLM → TTS pipelines and audio streaming
- **[advanced/UI_COMPONENTS.md](advanced/UI_COMPONENTS.md)** — Building streaming UIs with React/TypeScript
- **[advanced/MCP.md](advanced/MCP.md)** — Model Context Protocol for tool/resource import/export

### Development
- **[ROADMAP.md](ROADMAP.md)** — Vision, phases, implementation plan, and success metrics
- **[schema/](schema/)** — JSON Schema definitions, TypeScript types, and NDJSON fixtures

## Module Installation

```bash
go get github.com/shillcollin/gai
```

## Quick Start

### Basic Text Generation
```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/shillcollin/gai/core"
    "github.com/shillcollin/gai/providers/openai"
)

func main() {
    ctx := context.Background()

    // Initialize provider
    p := openai.New(
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        openai.WithModel("gpt-4o-mini"),
    )

    // Generate text
    res, err := p.GenerateText(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: "Write a haiku about Go programming"},
            }},
        },
        Temperature: 0.7,
        MaxTokens:   100,
    })

    if err != nil {
        panic(err)
    }

    fmt.Println(res.Text)
    fmt.Printf("Tokens used: %d\n", res.Usage.TotalTokens)
}
```

### Provider Agnostic Design
The same code works with any provider — just swap the constructor:

```go
// Anthropic
p := anthropic.New(
    anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
    anthropic.WithModel("claude-3-7-sonnet"),
)

// Gemini
p := gemini.New(
    gemini.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
    gemini.WithModel("gemini-1.5-flash"),
)

// OpenAI-Compatible (Groq, xAI, Cerebras, etc.)
p := compat.OpenAICompatible(compat.CompatOpts{
    BaseURL: "https://api.groq.com/openai/v1",
    APIKey:  os.Getenv("GROQ_API_KEY"),
})
```

## Key Capabilities

### 🔄 Streaming
Real-time streaming with normalized events across all providers:
```go
s, _ := p.StreamText(ctx, core.Request{Messages: msgs, Stream: true})
defer s.Close()

for ev := range s.Events() {
    switch ev.Type {
    case core.EventTextDelta:
        fmt.Print(ev.TextDelta)
    case core.EventToolCall:
        fmt.Printf("Tool: %s\n", ev.ToolCall.Name)
    case core.EventFinish:
        fmt.Printf("Done. Tokens: %d\n", ev.Usage.TotalTokens)
    }
}
```

### 🛠 Typed Tools & Multi-Step Execution
Define strongly-typed tools with automatic JSON Schema generation:
```go
type WeatherIn struct { Location string `json:"location"` }
type WeatherOut struct { TempF float64 `json:"temp_f"` }

getWeather := tools.New[WeatherIn, WeatherOut](
    "get_weather", "Get current temperature",
    func(ctx context.Context, in WeatherIn, meta tools.Meta) (WeatherOut, error) {
        return WeatherOut{TempF: 72.0}, nil
    },
)

runner := core.NewRunner(p)
res, _ := runner.ExecuteRequest(ctx, core.Request{
    Messages: msgs,
    Tools:    []core.ToolHandle{tools.NewCoreAdapter(getWeather)},
    StopWhen: core.NoMoreTools(),
})
```

### 📐 Structured Outputs
Type-safe JSON generation with automatic validation and repair:
```go
type Report struct {
    Title   string   `json:"title"`
    Summary string   `json:"summary"`
    Tags    []string `json:"tags"`
}

obj, _ := core.GenerateObjectTyped[Report](ctx, p, core.Request{
    Messages: msgs,
})
fmt.Printf("Report: %+v\n", obj.Value)
```

### 📊 Built-in Observability
OpenTelemetry integration with zero configuration:
```go
ctx := context.Background()
opts := obs.DefaultOptions()
opts.ServiceName = "my-app"
opts.Exporter = obs.ExporterStdout
if apiKey := os.Getenv("BRAINTRUST_API_KEY"); apiKey != "" {
    opts.Braintrust = obs.BraintrustOptions{Enabled: true, APIKey: apiKey}
}

shutdown, _ := obs.Init(ctx, opts)
defer shutdown(ctx)

// All provider calls, runner steps, and tool executions emit spans & metrics
```

## Documentation Standards

### Code Examples
All code examples in our documentation are:
- **Complete**: Full imports and error handling shown
- **Tested**: Examples are extracted and tested in CI
- **Practical**: Real-world scenarios, not toy examples
- **Progressive**: Build from simple to complex

### Provider Coverage
Every provider adapter includes:
- Setup and authentication
- Feature capabilities and limitations
- Provider-specific options via `ProviderOptions`
- Error mapping to common taxonomy
- Performance characteristics and costs

### API Stability
- **Core types**: Stable, backward-compatible (v1.x)
- **Provider adapters**: Stable interfaces, evolving options
- **Advanced features**: May evolve based on feedback
- **Experimental**: Clearly marked with stability warnings

## Contributing

See [ROADMAP.md](ROADMAP.md) for development priorities and phases. Key areas:
- Additional provider adapters
- Enhanced routing capabilities
- Memory and RAG helpers
- Performance optimizations

## Support

- **Issues**: [github.com/shillcollin/gai/issues](https://github.com/shillcollin/gai/issues)
- **Discussions**: [github.com/shillcollin/gai/discussions](https://github.com/shillcollin/gai/discussions)
- **Examples**: [github.com/shillcollin/gai/tree/main/examples](https://github.com/shillcollin/gai/tree/main/examples)

## License

MIT - See LICENSE file for details.

# gai - Go AI SDK

A production-ready Go SDK for building AI-powered applications. Provider-agnostic, type-safe, and observable.

## Features

- **Multi-Provider Support**: OpenAI, Anthropic, Gemini, Groq, xAI, and any OpenAI-compatible endpoint
- **Type-Safe Tools**: Generic tool framework with automatic JSON schema derivation from Go types
- **Normalized Streaming**: Unified `gai.events.v1` event schema across all providers (SSE/NDJSON)
- **Structured Outputs**: Type-safe structured generation with automatic repair
- **Multi-Step Runner**: Orchestrates tool execution with stop conditions and finalizers
- **Observability**: Built-in OpenTelemetry tracing and metrics
- **Multimodal**: Support for text, images, audio, video, and files

## Installation

```bash
go get github.com/shillcollin/gai
```

## Quick Start

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
    client := openai.New(
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        openai.WithModel("gpt-4o"),
    )

    result, err := client.GenerateText(context.Background(), core.Request{
        Messages: []core.Message{
            core.UserMessage(core.TextPart("Hello, world!")),
        },
    })
    if err != nil {
        panic(err)
    }

    fmt.Println(result.Text)
}
```

## Providers

| Provider | Package | Status |
|----------|---------|--------|
| OpenAI | `providers/openai` | Stable |
| OpenAI Responses API | `providers/openai-responses` | Stable |
| Anthropic | `providers/anthropic` | Stable |
| Google Gemini | `providers/gemini` | Stable |
| Groq | `providers/groq` | Stable |
| xAI | `providers/xai` | Stable |
| OpenAI-Compatible | `providers/compat` | Stable |

## Core Packages

- **`core/`** - Provider interface, messages, parts, requests, streaming, structured outputs
- **`providers/`** - Provider implementations
- **`runner/`** - Multi-step tool execution orchestrator
- **`tools/`** - Type-safe tool framework
- **`stream/`** - Streaming helpers for SSE/NDJSON
- **`obs/`** - OpenTelemetry integration
- **`prompts/`** - Versioned prompt registry

## Documentation

- [Developer Guide](docs/DEVELOPER_GUIDE.md)
- [API Reference](docs/API_REFERENCE.md)
- [Roadmap](docs/ROADMAP.md)
- [Testing Guide](docs/TESTING.md)

## Examples

See the [examples/](examples/) directory for runnable examples:

- `examples/basic/` - Basic text generation
- `examples/streaming/` - Streaming responses
- `examples/structured/` - Structured outputs
- `examples/tools/` - Tool usage
- `examples/observability/` - OpenTelemetry integration

## License

MIT

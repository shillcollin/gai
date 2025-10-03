# gai Developer Guide

The comprehensive guide for building production AI applications with gai — the Go-first AI SDK that provides exceptional developer experience, broad functionality, and top-tier performance.

## Table of Contents

1. [Introduction & Philosophy](#introduction--philosophy)
2. [Installation & Setup](#installation--setup)
3. [Core Concepts](#core-concepts)
4. [Basic Text Generation](#basic-text-generation)
5. [Streaming Responses](#streaming-responses)
6. [Structured Outputs](#structured-outputs)
7. [Tools & Multi-Step Execution](#tools--multi-step-execution)
8. [Prompt Management](#prompt-management)
9. [Observability & Monitoring](#observability--monitoring)
10. [Error Handling & Retries](#error-handling--retries)
11. [Performance Optimization](#performance-optimization)
12. [Testing Strategies](#testing-strategies)
13. [Production Best Practices](#production-best-practices)
14. [Advanced Patterns](#advanced-patterns)
15. [Migration Guide](#migration-guide)

## Introduction & Philosophy

### Why gai?

The gai SDK addresses fundamental challenges in building production AI applications:

**Provider Lock-in Prevention**: Traditional AI SDKs tie you to specific providers. With gai, switching between OpenAI, Anthropic, Gemini, or any OpenAI-compatible provider requires changing only the constructor — your application logic remains unchanged.

**Type Safety Without Reflection**: Go's compile-time type system ensures correctness. gai leverages generics for structured outputs and typed tools, eliminating runtime reflection and catching errors at compile time.

**Production-First Design**: Built-in observability, comprehensive error handling, retry mechanisms, and performance optimizations make gai ready for production from day one.

**Streaming as a First-Class Citizen**: All providers emit normalized events following the `gai.events.v1` specification, making real-time UIs consistent regardless of backend.

### Design Principles

1. **Provider Agnostic**: Core types and interfaces work identically across all providers
2. **Go-First**: Native Go patterns, no wrapper abstractions or unnecessary complexity
3. **Observable by Default**: OpenTelemetry integration provides insights without additional code
4. **Fail Gracefully**: Comprehensive error taxonomy and retry strategies handle transient failures
5. **Performance Conscious**: Zero-allocation hot paths, connection pooling, and efficient streaming

## Installation & Setup

### Basic Installation

```bash
go get github.com/shillcollin/gai
```

### Module Structure

The SDK is organized into logical packages:

```go
import (
    "github.com/shillcollin/gai/core"           // Core types and interfaces
    "github.com/shillcollin/gai/providers/openai"         // OpenAI Chat adapter
    openairesponses "github.com/shillcollin/gai/providers/openai-responses" // OpenAI Responses adapter
    "github.com/shillcollin/gai/providers/anthropic"  // Anthropic adapter
    "github.com/shillcollin/gai/providers/gemini"     // Gemini adapter
    "github.com/shillcollin/gai/providers/compat"     // OpenAI-compatible adapters
    "github.com/shillcollin/gai/tools"          // Typed tool definitions
    "github.com/shillcollin/gai/prompts"        // Prompt management
    "github.com/shillcollin/gai/obs"            // Observability
    "github.com/shillcollin/gai/stream"         // Stream helpers
)
```

### Environment Configuration

Create a `.env` file for development:

```bash
# Provider API Keys
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
GROQ_API_KEY=...

# Optional: Observability
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_SERVICE_NAME=my-ai-app

# Optional: Feature Flags
GAI_LIVE=1  # Enable live provider tests
```

### Provider Initialization

Each provider follows the same initialization pattern:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/shillcollin/gai/core"
    "github.com/shillcollin/gai/providers/openai"
    "github.com/shillcollin/gai/providers/anthropic"
    "github.com/shillcollin/gai/providers/gemini"
    "github.com/shillcollin/gai/providers/compat"
)

func initProviders() {
    ctx := context.Background()

    // OpenAI Chat Completions
    openaiProvider := openai.New(
        openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        openai.WithModel("gpt-4o-mini"),
        openai.WithBaseURL("https://api.openai.com/v1"), // Optional: custom endpoint
        openai.WithHTTPClient(customHTTPClient),         // Optional: custom HTTP client
    )

    // OpenAI Responses API (GPT-5, o-series)
    responsesProvider := openairesponses.New(
        openairesponses.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        openairesponses.WithModel("gpt-5-nano"), // Automatically normalizes max_completion_tokens, reasoning, etc.
    )

    // Anthropic
    anthropicProvider := anthropic.New(
        anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
        anthropic.WithModel("claude-3-7-sonnet"),
        anthropic.WithMaxRetries(3),
    )

    // Gemini
    geminiProvider := gemini.New(
        gemini.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
        gemini.WithModel("gemini-1.5-pro"),
        gemini.WithRegion("us-central1"), // Optional: specific region
    )

    // OpenAI Responses API usage (e.g. GPT-5, o-series)
    respResult, err := responsesProvider.GenerateText(ctx, core.Request{
        Model:     "gpt-5-nano",
        MaxTokens: 1200,             // renamed to max_completion_tokens for you
        Messages: []core.Message{
            core.UserMessage(core.TextPart("Summarize the latest AI governance research")),
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    for _, w := range respResult.Warnings {
        log.Printf("OpenAI Responses warning: %s (%s)", w.Message, w.Field)
    }

    // OpenAI-Compatible (Groq example)
    groqProvider := compat.OpenAICompatible(compat.CompatOpts{
        BaseURL: "https://api.groq.com/openai/v1",
        APIKey:  os.Getenv("GROQ_API_KEY"),
        Model:   "llama-3.3-70b",
        DisableJSONStreaming: true, // Some providers don't support JSON streaming
    })
}
```

## Core Concepts

### Provider Interface

All providers implement the `core.Provider` interface, ensuring consistent behavior:

```go
type Provider interface {
    // Text generation
    GenerateText(context.Context, Request) (*TextResult, error)
    StreamText(context.Context, Request) (*Stream, error)

    // Structured outputs (raw JSON)
    GenerateObject(context.Context, Request) (*ObjectResultRaw, error)
    StreamObject(context.Context, Request) (*ObjectStreamRaw, error)

    // Capabilities discovery
    Capabilities() Capabilities
}
```

### Messages and Parts

gai uses a multimodal message model that supports text, images, audio, video, files, and tool interactions:

```go
// Basic text message
textMsg := core.Message{
    Role: core.User,
    Parts: []core.Part{
        core.Text{Text: "Explain quantum computing"},
    },
}

// Multimodal message with image
multimodalMsg := core.Message{
    Role: core.User,
    Parts: []core.Part{
        core.Text{Text: "What's in this image?"},
        core.ImageURL{
            URL:  "https://example.com/photo.jpg",
            MIME: "image/jpeg",
        },
    },
}

// Message with local image bytes
localImageMsg := core.Message{
    Role: core.User,
    Parts: []core.Part{
        core.Text{Text: "Analyze this chart"},
        core.Image{
            Source: core.BlobRef{
                Kind:  core.BlobBytes,
                Bytes: imageBytes,
                MIME:  "image/png",
            },
        },
    },
}

// System message for instructions
systemMsg := core.Message{
    Role: core.System,
    Parts: []core.Part{
        core.Text{Text: "You are a helpful coding assistant. Provide concise, idiomatic Go code."},
    },
}
```

### Request Configuration

The `core.Request` struct centralizes all generation parameters:

```go
req := core.Request{
    // Model selection
    Model: "gpt-4o-mini",

    // Conversation history
    Messages: []core.Message{
        {Role: core.System, Parts: []core.Part{core.Text{Text: "Be concise"}}},
        {Role: core.User, Parts: []core.Part{core.Text{Text: "Hello"}}},
    },

    // Generation parameters
    Temperature: 0.7,      // Randomness (0.0 = deterministic, 2.0 = very random)
    MaxTokens:   1000,     // Maximum output tokens
    TopP:        0.9,      // Nucleus sampling
    Stream:      false,    // Enable streaming

    // Safety configuration
    Safety: &core.SafetyConfig{
        Harassment: core.SafetyBlockMedium,
        Sexual:     core.SafetyBlockHigh,
        Dangerous:  core.SafetyBlockLow,
    },

    // Session management (for caching)
    Session: &core.Session{
        Provider: "gemini",
        ID:       "session-123",
    },

    // Provider-specific options
    ProviderOptions: map[string]any{
        "openai.json_mode": "strict",
        "gemini.grounding": "web",
    },

    // Tools configuration
    Tools: []core.ToolHandle{/* tool definitions */},
    ToolChoice: core.ToolAuto, // Auto, None, or Required

    // Metadata for observability
    Metadata: map[string]any{
        "user_id":    "user-456",
        "request_id": "req-789",
        "feature":    "chat",
    },
}
```

## Basic Text Generation

### Simple Generation

The most basic use case — generating text from a prompt:

```go
package main

import (
    "context"
    "fmt"
    "log"
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
    result, err := p.GenerateText(ctx, core.Request{
        Messages: []core.Message{
            {
                Role: core.User,
                Parts: []core.Part{
                    core.Text{Text: "Write a limerick about Go programming"},
                },
            },
        },
        Temperature: 0.8,
        MaxTokens:   150,
    })

    if err != nil {
        log.Fatalf("Generation failed: %v", err)
    }

    fmt.Println("Response:", result.Text)
    fmt.Printf("Tokens - Input: %d, Output: %d, Total: %d\n",
        result.Usage.InputTokens,
        result.Usage.OutputTokens,
        result.Usage.TotalTokens)
    fmt.Printf("Estimated cost: $%.4f\n", result.Usage.CostUSD)
}
```

### Conversation Management

Managing multi-turn conversations with context:

```go
type Conversation struct {
    messages []core.Message
    provider core.Provider
}

func NewConversation(p core.Provider) *Conversation {
    return &Conversation{
        provider: p,
        messages: []core.Message{
            {
                Role: core.System,
                Parts: []core.Part{
                    core.Text{Text: "You are a helpful assistant. Be concise but thorough."},
                },
            },
        },
    }
}

func (c *Conversation) SendMessage(ctx context.Context, text string) (string, error) {
    // Add user message to history
    c.messages = append(c.messages, core.Message{
        Role: core.User,
        Parts: []core.Part{core.Text{Text: text}},
    })

    // Generate response
    result, err := c.provider.GenerateText(ctx, core.Request{
        Messages:    c.messages,
        Temperature: 0.7,
        MaxTokens:   500,
    })

    if err != nil {
        return "", fmt.Errorf("generation failed: %w", err)
    }

    // Add assistant response to history
    c.messages = append(c.messages, core.Message{
        Role: core.Assistant,
        Parts: []core.Part{core.Text{Text: result.Text}},
    })

    return result.Text, nil
}

func (c *Conversation) GetHistory() []core.Message {
    return c.messages
}

func (c *Conversation) Reset() {
    // Keep only system message
    if len(c.messages) > 0 && c.messages[0].Role == core.System {
        c.messages = c.messages[:1]
    } else {
        c.messages = nil
    }
}

// Usage
func main() {
    ctx := context.Background()
    p := openai.New(openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")))

    conv := NewConversation(p)

    // First message
    response1, _ := conv.SendMessage(ctx, "What is the capital of France?")
    fmt.Println("Assistant:", response1)

    // Follow-up with context
    response2, _ := conv.SendMessage(ctx, "What is its population?")
    fmt.Println("Assistant:", response2)

    // The conversation maintains context
    fmt.Printf("Conversation has %d messages\n", len(conv.GetHistory()))
}
```

### Provider-Specific Features

Access advanced features using type-safe builders from each provider's package. This is the recommended way to set `ProviderOptions` as it prevents typos and is validated by the compiler.

```go
import (
    "github.com/shillcollin/gai/providers/openai"
    "github.com/shillcollin/gai/providers/gemini"
    "github.com/shillcollin/gai/providers/anthropic"
)

// OpenAI: JSON mode and deterministic generation
result, err := openaiProvider.GenerateText(ctx, core.Request{
    Messages: msgs,
    ProviderOptions: openai.BuildProviderOptions(
        openai.WithJSONMode(),
        openai.WithSeed(42),
    ),
})

// Gemini: Grounding and thinking features
result, err := geminiProvider.GenerateText(ctx, core.Request{
    Messages: msgs,
    ProviderOptions: gemini.BuildProviderOptions(
        gemini.WithGrounding("web"),
        gemini.WithThoughtSignatures("require"),
        gemini.WithThinkingBudget(2048),
        gemini.WithIncludeThoughts(true),
    ),
})

// Anthropic: Thinking models
result, err := anthropicProvider.GenerateText(ctx, core.Request{
    Messages: msgs,
    ProviderOptions: anthropic.BuildProviderOptions(
        anthropic.WithThinkingEnabled(true),
    ),
})

// For brand-new or experimental features not yet in the SDK,
// you can still add them to the map directly.
opts := openai.BuildProviderOptions(openai.WithSeed(42))
opts["openai.hypothetical_new_param"] = "some_value"
result, err := openaiProvider.GenerateText(ctx, core.Request{
    Messages: msgs,
    ProviderOptions: opts,
})
``````

## Streaming Responses

### Basic Streaming

Streaming provides real-time output as it's generated:

```go
func streamExample(ctx context.Context, p core.Provider) error {
    // Create streaming request
    stream, err := p.StreamText(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: "Tell me a story about a robot"},
            }},
        },
        Stream:      true,
        Temperature: 0.8,
        MaxTokens:   500,
    })

    if err != nil {
        return fmt.Errorf("failed to start stream: %w", err)
    }
    defer stream.Close()

    // Process events
    for event := range stream.Events() {
        switch event.Type {
        case core.EventTextDelta:
            // Print text as it arrives
            fmt.Print(event.TextDelta)

        case core.EventToolCall:
            fmt.Printf("\n[Tool call] %s -> %v\n", event.ToolCall.Name, event.ToolCall.Input)
            if sig, ok := event.ToolCall.Metadata["gemini.thought_signature"]; ok {
                fmt.Println("(Gemini thought signature captured)")
            }

        case core.EventFinish:
            // Stream completed successfully
            fmt.Printf("\n\nCompleted. Tokens used: %d\n",
                event.Usage.TotalTokens)

        case core.EventError:
            // Handle streaming error
            return fmt.Errorf("stream error: %w", event.Err)
        }
    }

    return nil
}
```

When a Gemini model produces a tool call, the stream event's `ToolCall.Metadata`
contains the opaque `gemini.thought_signature` value and the raw content blob the
provider expects on the next request. Always return the prior model message
unchanged (including that signature) when you send the tool result back so the
model can resume the same reasoning thread.

### Advanced Stream Processing

Handling all event types with full context:

```go
type StreamProcessor struct {
    fullText     strings.Builder
    reasoning    strings.Builder
    toolCalls    []core.ToolCall
    citations    []core.Citation
    safetyEvents []core.SafetyEvent
    usage        core.Usage
}

func (sp *StreamProcessor) ProcessStream(ctx context.Context, stream *core.Stream) error {
    defer stream.Close()

    for event := range stream.Events() {
        switch event.Type {
        case core.EventStart:
            fmt.Printf("Stream started with capabilities: %v\n", event.Capabilities)
            fmt.Printf("Reasoning visibility: %s\n", event.Policies.ReasoningVisibility)

        case core.EventTextDelta:
            sp.fullText.WriteString(event.TextDelta)
            fmt.Print(event.TextDelta) // Real-time output

        case core.EventReasoningDelta:
            // Only available when reasoning visibility is "raw"
            sp.reasoning.WriteString(event.Text)

        case core.EventReasoningSummary:
            fmt.Printf("\n[Reasoning Summary] %s\n", event.Summary)

        case core.EventToolCall:
            sp.toolCalls = append(sp.toolCalls, event.ToolCall)
            fmt.Printf("\n[Tool Call] %s with input: %v\n",
                event.ToolCall.Name, event.ToolCall.Input)

        case core.EventToolResult:
            fmt.Printf("[Tool Result] %s: %v\n",
                event.ToolResult.CallID, event.ToolResult.Result)

        case core.EventCitations:
            sp.citations = append(sp.citations, event.Citations...)
            for _, cite := range event.Citations {
                fmt.Printf("\n[Citation] %s - %s\n", cite.Title, cite.URI)
            }

        case core.EventSafety:
            sp.safetyEvents = append(sp.safetyEvents, event.Safety)
            fmt.Printf("\n[Safety] Category: %s, Action: %s, Score: %.2f\n",
                event.Safety.Category, event.Safety.Action, event.Safety.Score)

        case core.EventStepStart:
            fmt.Printf("\n[Step %d Started]\n", event.StepID)

        case core.EventStepFinish:
            fmt.Printf("[Step %d Completed] Duration: %dms, Tools: %d\n",
                event.StepID, event.DurationMS, event.ToolCalls)

        case core.EventFinish:
            sp.usage = event.Usage
            fmt.Printf("\n\n[Stream Finished]\n")
            fmt.Printf("Reason: %s\n", event.FinishReason.Type)
            fmt.Printf("Total tokens: %d (Input: %d, Output: %d)\n",
                event.Usage.TotalTokens,
                event.Usage.InputTokens,
                event.Usage.OutputTokens)
            if event.Usage.ReasoningTokens > 0 {
                fmt.Printf("Reasoning tokens: %d\n", event.Usage.ReasoningTokens)
            }
            fmt.Printf("Estimated cost: $%.4f\n", event.Usage.CostUSD)

        case core.EventError:
            return fmt.Errorf("stream error: %w", event.Err)
        }
    }

    return nil
}

// Get final results
func (sp *StreamProcessor) GetResults() (string, core.Usage) {
    return sp.fullText.String(), sp.usage
}
```

### Streaming with Backpressure

Managing stream consumption rate:

```go
func streamWithBackpressure(ctx context.Context, p core.Provider) error {
    stream, err := p.StreamText(ctx, core.Request{
        Messages: msgs,
        Stream:   true,
    })
    if err != nil {
        return err
    }
    defer stream.Close()

    // Create buffered channel for processing
    processQueue := make(chan core.StreamEvent, 10)
    errChan := make(chan error, 1)

    // Start processor goroutine
    go func() {
        for event := range processQueue {
            // Simulate slow processing
            time.Sleep(50 * time.Millisecond)

            switch event.Type {
            case core.EventTextDelta:
                fmt.Print(event.TextDelta)
            case core.EventError:
                errChan <- event.Err
                return
            case core.EventFinish:
                close(errChan)
                return
            }
        }
    }()

    // Feed events to processor with backpressure
    for event := range stream.Events() {
        select {
        case processQueue <- event:
            // Event queued successfully
        case <-ctx.Done():
            return ctx.Err()
        }
    }

    close(processQueue)
    return <-errChan
}
```

## Structured Outputs

### Basic Structured Output

Generate validated JSON directly into Go types:

```go
// Define your schema with struct tags
type ProductReview struct {
    ProductName string  `json:"product_name"`
    Rating      int     `json:"rating" minimum:"1" maximum:"5"`
    Pros        []string `json:"pros"`
    Cons        []string `json:"cons"`
    Summary     string  `json:"summary"`
    Recommended bool    `json:"recommended"`
}

func generateReview(ctx context.Context, p core.Provider) (*ProductReview, error) {
    result, err := core.GenerateObjectTyped[ProductReview](ctx, p, core.Request{
        Messages: []core.Message{
            {Role: core.System, Parts: []core.Part{
                core.Text{Text: "Generate a product review in JSON format"},
            }},
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: "Review the iPhone 15 Pro"},
            }},
        },
        Temperature: 0.7,
        ProviderOptions: map[string]any{
            "openai.json_mode": "strict", // Use provider's strict mode
        },
    })

    if err != nil {
        return nil, fmt.Errorf("failed to generate review: %w", err)
    }

    // result.Value is already typed as ProductReview
    fmt.Printf("Product: %s\n", result.Value.ProductName)
    fmt.Printf("Rating: %d/5\n", result.Value.Rating)
    fmt.Printf("Recommended: %v\n", result.Value.Recommended)

    return &result.Value, nil
}
```

### Complex Nested Structures

Working with deeply nested JSON structures:

```go
type Address struct {
    Street     string `json:"street"`
    City       string `json:"city"`
    State      string `json:"state"`
    PostalCode string `json:"postal_code"`
    Country    string `json:"country"`
}

type ContactInfo struct {
    Email       string   `json:"email" format:"email"`
    Phone       string   `json:"phone"`
    LinkedIn    string   `json:"linkedin,omitempty"`
    GitHub      string   `json:"github,omitempty"`
    Websites    []string `json:"websites,omitempty"`
}

type Experience struct {
    Company     string    `json:"company"`
    Position    string    `json:"position"`
    StartDate   string    `json:"start_date" format:"date"`
    EndDate     string    `json:"end_date,omitempty" format:"date"`
    Current     bool      `json:"current"`
    Description string    `json:"description"`
    Achievements []string `json:"achievements"`
}

type Education struct {
    Institution string `json:"institution"`
    Degree      string `json:"degree"`
    Field       string `json:"field"`
    GraduationYear int `json:"graduation_year"`
    GPA         float64 `json:"gpa,omitempty" minimum:"0" maximum:"4"`
}

type Resume struct {
    Name        string       `json:"name"`
    Title       string       `json:"title"`
    Summary     string       `json:"summary"`
    Contact     ContactInfo  `json:"contact"`
    Address     Address      `json:"address"`
    Experience  []Experience `json:"experience"`
    Education   []Education  `json:"education"`
    Skills      []string     `json:"skills"`
    Languages   []string     `json:"languages"`
    Certifications []string `json:"certifications,omitempty"`
}

func generateResume(ctx context.Context, p core.Provider, description string) (*Resume, error) {
    result, err := core.GenerateObjectTyped[Resume](ctx, p, core.Request{
        Messages: []core.Message{
            {Role: core.System, Parts: []core.Part{
                core.Text{Text: "Generate a detailed professional resume in JSON format"},
            }},
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: description},
            }},
        },
        Temperature: 0.3, // Lower temperature for more consistent structure
        MaxTokens:   2000,
    })

    if err != nil {
        return nil, fmt.Errorf("failed to generate resume: %w", err)
    }

    // Validate and process the resume
    resume := &result.Value

    // Sort experience by date
    sort.Slice(resume.Experience, func(i, j int) bool {
        return resume.Experience[i].StartDate > resume.Experience[j].StartDate
    })

    return resume, nil
}
```

### Streaming Structured Outputs

Stream JSON objects with progressive validation:

```go
func streamStructuredOutput[T any](ctx context.Context, p core.Provider, prompt string) (*T, error) {
    stream, err := core.StreamObjectTyped[T](ctx, p, core.Request{
        Messages: []core.Message{
            {Role: core.User, Parts: []core.Part{core.Text{Text: prompt}}},
        },
        Stream: true,
    })

    if err != nil {
        return nil, fmt.Errorf("failed to start stream: %w", err)
    }
    defer stream.Close()

    // Monitor streaming progress
    eventCount := 0
    for event := range stream.Events() {
        eventCount++
        if eventCount%10 == 0 {
            fmt.Printf("Received %d events...\n", eventCount)
        }

        if event.Type == core.EventError {
            return nil, fmt.Errorf("stream error: %w", event.Err)
        }
    }

    // Get final validated object
    result, err := stream.Final()
    if err != nil {
        return nil, fmt.Errorf("failed to get final object: %w", err)
    }

    return &result.Value, nil
}
```

### Validation and Repair

The SDK automatically validates and repairs malformed JSON:

```go
type StrictConfig struct {
    Mode core.StrictMode
}

func generateWithValidation(ctx context.Context, p core.Provider) error {
    type DataPoint struct {
        Timestamp string  `json:"timestamp" format:"date-time"`
        Value     float64 `json:"value"`
        Unit      string  `json:"unit" enum:"celsius,fahrenheit,kelvin"`
        Valid     bool    `json:"valid"`
    }

    // Try different validation modes
    modes := []core.StrictMode{
        core.StrictProvider,  // Use provider's strict mode only
        core.StrictRepair,    // Repair malformed JSON
        core.StrictBoth,      // Provider strict with repair fallback
    }

    for _, mode := range modes {
        result, err := core.GenerateObjectTyped[DataPoint](ctx, p, core.Request{
            Messages: msgs,
            ProviderOptions: map[string]any{
                "validation_mode": mode,
            },
        })

        if err != nil {
            fmt.Printf("Mode %v failed: %v\n", mode, err)
            continue
        }

        fmt.Printf("Mode %v succeeded: %+v\n", mode, result.Value)
    }

    return nil
}
```

## Tools & Multi-Step Execution

### Defining Typed Tools

Tools are strongly-typed functions that models can call:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/shillcollin/gai/tools"
)

// Define input/output types for each tool
type WeatherInput struct {
    Location string `json:"location" description:"City and state/country"`
    Units    string `json:"units,omitempty" enum:"celsius,fahrenheit" default:"celsius"`
}

type WeatherOutput struct {
    Temperature float64 `json:"temperature"`
    Conditions  string  `json:"conditions"`
    Humidity    int     `json:"humidity"`
    WindSpeed   float64 `json:"wind_speed"`
    Units       string  `json:"units"`
}

// Create the weather tool
var getWeather = tools.New[WeatherInput, WeatherOutput](
    "get_weather",
    "Get current weather for a location",
    func(ctx context.Context, in WeatherInput, meta tools.Meta) (WeatherOutput, error) {
        // Log tool invocation
        fmt.Printf("[Tool Call %s] Getting weather for %s\n", meta.CallID, in.Location)

        // Simulate API call
        // In production, call actual weather API
        return WeatherOutput{
            Temperature: 22.5,
            Conditions:  "Partly cloudy",
            Humidity:    65,
            WindSpeed:   12.3,
            Units:       in.Units,
        }, nil
    },
)

// More complex tool with external API
type SearchInput struct {
    Query    string   `json:"query" description:"Search query"`
    MaxResults int    `json:"max_results,omitempty" default:"5" minimum:"1" maximum:"20"`
    Domains  []string `json:"domains,omitempty" description:"Limit to specific domains"`
}

type SearchResult struct {
    Title   string `json:"title"`
    URL     string `json:"url"`
    Snippet string `json:"snippet"`
}

type SearchOutput struct {
    Results []SearchResult `json:"results"`
    Total   int           `json:"total"`
}

var webSearch = tools.New[SearchInput, SearchOutput](
    "web_search",
    "Search the web for information",
    func(ctx context.Context, in SearchInput, meta tools.Meta) (SearchOutput, error) {
        // Set timeout for external API call
        ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
        defer cancel()

        // Build search request
        req, err := http.NewRequestWithContext(ctx, "GET",
            fmt.Sprintf("https://api.example.com/search?q=%s&limit=%d",
                in.Query, in.MaxResults), nil)
        if err != nil {
            return SearchOutput{}, fmt.Errorf("failed to create request: %w", err)
        }

        // Make API call
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return SearchOutput{}, fmt.Errorf("search failed: %w", err)
        }
        defer resp.Body.Close()

        // Parse response
        var output SearchOutput
        if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
            return SearchOutput{}, fmt.Errorf("failed to parse response: %w", err)
        }

        return output, nil
    },
)
```

### Using the Runner for Multi-Step Execution

The Runner orchestrates tool execution across multiple steps:

```go
func runWithTools(ctx context.Context, p core.Provider) error {
    // Configure the runner
    runner := core.NewRunner(p,
        core.WithMaxParallel(4),              // Max concurrent tool executions
        core.WithToolTimeout(30*time.Second), // Timeout per tool call
        core.WithOnToolError(core.ToolErrorAppendAndContinue), // Continue on errors
    )

    // Execute request with tools
    result, err := runner.ExecuteRequest(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: "What's the weather in Paris and Tokyo? Compare them."},
            }},
        },
        Tools: []core.ToolHandle{
            tools.NewCoreAdapter(getWeather),
            tools.NewCoreAdapter(webSearch),
        },
        ToolChoice: core.ToolAuto,    // Let model decide when to use tools
        StopWhen: core.CombineConditions(
            core.MaxSteps(5),          // Maximum 5 steps
            core.NoMoreTools(),        // Stop when no more tools needed
        ),
    })

    if err != nil {
        return fmt.Errorf("execution failed: %w", err)
    }

    fmt.Println("Final response:", result.Text)
    fmt.Printf("Steps taken: %d\n", len(result.Steps))
    fmt.Printf("Total tokens: %d\n", result.Usage.TotalTokens)

    // Inspect individual steps
    for i, step := range result.Steps {
        fmt.Printf("\nStep %d:\n", i+1)
        fmt.Printf("  Tool calls: %d\n", len(step.ToolCalls))
        fmt.Printf("  Text length: %d\n", len(step.Text))
        fmt.Printf("  Duration: %dms\n", step.DurationMS)
    }

    return nil
}
```

### Advanced Stop Conditions

Control when the runner should stop:

```go
// Custom stop condition
func stopOnKeyword(keyword string) core.StopCondition {
    return func(state core.State) (bool, core.StopReason) {
        if strings.Contains(state.LastText(), keyword) {
            return true, core.StopReason{
                Type:        "keyword_found",
                Description: fmt.Sprintf("Found keyword: %s", keyword),
            }
        }
        return false, core.StopReason{}
    }
}

// Combine multiple conditions
result, err := runner.ExecuteRequest(ctx, core.Request{
    Messages: msgs,
    Tools:    tools,
    StopWhen: core.Any(
        core.MaxSteps(10),                    // Stop after 10 steps
        core.NoMoreTools(),                   // Stop when no tools needed
        core.UntilToolSeen("final_answer"),   // Stop when specific tool used
        core.UntilTextLength(2000),           // Stop at text length
        stopOnKeyword("DONE"),                // Custom condition
    ),
})
```

### Tool Execution Patterns

#### Parallel Tool Execution

Tools within the same step execute concurrently:

```go
// Tools that can run in parallel
var toolA = tools.New[InputA, OutputA]("tool_a", "Tool A",
    func(ctx context.Context, in InputA, meta tools.Meta) (OutputA, error) {
        time.Sleep(2 * time.Second) // Simulated work
        return OutputA{Result: "A"}, nil
    },
)

var toolB = tools.New[InputB, OutputB]("tool_b", "Tool B",
    func(ctx context.Context, in InputB, meta tools.Meta) (OutputB, error) {
        time.Sleep(2 * time.Second) // Simulated work
        return OutputB{Result: "B"}, nil
    },
)

// When the model calls both tools in one step, they run in parallel
// Total time: ~2 seconds instead of 4
```

#### Tool Memoization

Cache tool results to avoid redundant calls:

```go
type MemCache struct {
    mu    sync.RWMutex
    cache map[string]any
}

func (m *MemCache) Get(name, inputHash string) (any, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    key := fmt.Sprintf("%s:%s", name, inputHash)
    val, ok := m.cache[key]
    return val, ok
}

func (m *MemCache) Set(name, inputHash string, result any) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.cache == nil {
        m.cache = make(map[string]any)
    }
    key := fmt.Sprintf("%s:%s", name, inputHash)
    m.cache[key] = result
}

// Use with runner
memo := &MemCache{}
runner := core.NewRunner(p,
    core.WithMemo(memo),
)
```

#### Tool Retry Logic

Implement retry for transient failures:

```go
runner := core.NewRunner(p,
    core.WithToolRetry(core.ToolRetry{
        MaxAttempts: 3,
        BaseDelay:   100 * time.Millisecond,
        Jitter:      true,
    }),
)

// Tool with built-in retry awareness
var resilientTool = tools.New[Input, Output](
    "resilient_tool",
    "Tool with retry logic",
    func(ctx context.Context, in Input, meta tools.Meta) (Output, error) {
        // Check if this is a retry
        attempt := meta.Metadata["retry_attempt"].(int)
        if attempt > 0 {
            fmt.Printf("Retry attempt %d for call %s\n", attempt, meta.CallID)
        }

        // Implement exponential backoff for external services
        if attempt > 0 {
            delay := time.Duration(attempt) * time.Second
            time.Sleep(delay)
        }

        // Actual tool logic here
        return Output{}, nil
    },
)
```

### Finalizers (OnStop)

Process results after execution completes:

```go
result, err := runner.ExecuteRequest(ctx, core.Request{
    Messages: msgs,
    Tools:    tools,
    StopWhen: core.MaxSteps(5),
    OnStop: func(ctx context.Context, state core.FinalState) (*core.TextResult, error) {
        // Analyze the execution
        totalToolCalls := 0
        for _, step := range state.Steps {
            totalToolCalls += len(step.ToolCalls)
        }

        // Generate summary based on stop reason
        var summary string
        switch state.StopReason.Type {
        case core.StopReasonMaxSteps:
            summary = fmt.Sprintf(
                "Reached maximum steps. Completed %d tool calls across %d steps.",
                totalToolCalls, len(state.Steps))
        case core.StopReasonNoMoreTools:
            summary = fmt.Sprintf(
                "Task completed successfully with %d tool calls.",
                totalToolCalls)
        default:
            summary = "Task completed."
        }

        // Return modified result
        return &core.TextResult{
            Text:  fmt.Sprintf("%s\n\n%s", state.LastText(), summary),
            Steps: state.Steps,
            Usage: state.Usage,
        }, nil
    },
})
```

When you run the streaming API (`runner.StreamRequest`) the finalizer now executes before the stream closes. Any additional steps returned by the finalizer (for example, a synthetic answer when a stop condition fires before the model can respond) are converted into streamed events so the client still receives a final assistant message.

## Prompt Management

### Versioned Prompts with Templates

Manage prompts as versioned templates with runtime flexibility:

```go
import (
    "context"
    "embed"
    "fmt"

    "github.com/shillcollin/gai/prompts"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

func setupPrompts() (*prompts.Registry, error) {
    // Create registry with embedded prompts
    reg := prompts.NewRegistry(
        promptFS,
        prompts.WithOverrideDir("./prompts_override"), // Hot reload in dev
        prompts.WithHelperFunc("truncate", func(s string, n int) string {
            if len(s) <= n {
                return s
            }
            return s[:n] + "..."
        }),
        prompts.WithHelperFunc("format_date", func(t time.Time) string {
            return t.Format("January 2, 2006")
        }),
    )

    // Validate all prompts on startup
    if err := reg.Reload(); err != nil {
        return nil, fmt.Errorf("prompt validation failed: %w", err)
    }

    return reg, nil
}
```

### Prompt Templates

Create versioned prompt templates:

```
# prompts/summarize@1.0.0.tmpl
You are a document summarizer. Your task is to create {{.Style}} summaries.

Requirements:
- Maximum {{.MaxWords}} words
- Include {{.BulletPoints}} key points
- Target audience: {{.Audience}}
{{if .IncludeCitations}}
- Include citations in [1] format
{{end}}

Document to summarize:
{{.Document}}
```

```
# prompts/code_review@2.1.0.tmpl
Review the following {{.Language}} code for:
1. Correctness and bug risks
2. Performance issues
3. Security vulnerabilities
4. Code style and best practices
5. Test coverage gaps

{{if .FocusArea}}
Pay special attention to: {{.FocusArea}}
{{end}}

Code:
```{{.Language}}
{{.Code}}
```

Provide your review in the following format:
- Overall Assessment: [brief summary]
- Critical Issues: [list any bugs or security issues]
- Improvements: [suggested enhancements]
- Positive Aspects: [what's done well]
```

### Using Prompts in Code
```
```go

func usePrompts(ctx context.Context, reg *prompts.Registry, p core.Provider) error {
    // Render prompt with variables
    text, promptID, err := reg.Render(ctx, "summarize", "1.0.0", map[string]any{
        "Style":            "concise",
        "MaxWords":         150,
        "BulletPoints":     5,
        "Audience":         "technical",
        "IncludeCitations": true,
        "Document":         documentText,
    })

    if err != nil {
        return fmt.Errorf("failed to render prompt: %w", err)
    }

    // promptID contains name, version, and fingerprint for tracking
    fmt.Printf("Using prompt: %s v%s (fingerprint: %s)\n",
        promptID.Name, promptID.Version, promptID.Fingerprint)

    // Use the rendered prompt
    result, err := p.GenerateText(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.System, Parts: []core.Part{core.Text{Text: text}}},
            {Role: core.User, Parts: []core.Part{core.Text{Text: "Summarize this"}}},
        },
        Metadata: map[string]any{
            "prompt_id": promptID, // Attach for observability
        },
    })

    return nil
}
```

### Prompt Versioning Strategy

```go
// Semantic versioning for prompts
// MAJOR: Breaking changes (different output format)
// MINOR: New features or significant changes
// PATCH: Minor tweaks and fixes

func selectPromptVersion(reg *prompts.Registry, name string, env string) (string, error) {
    versions := reg.ListVersions(name)
    if len(versions) == 0 {
        return "", fmt.Errorf("no versions found for prompt %s", name)
    }

    switch env {
    case "production":
        // Use stable version
        return findStableVersion(versions), nil
    case "staging":
        // Use latest version
        return versions[len(versions)-1], nil
    case "development":
        // Use override if exists, otherwise latest
        if override := reg.HasOverride(name); override != "" {
            return override, nil
        }
        return versions[len(versions)-1], nil
    default:
        return versions[0], nil
    }
}
```

### Provider Notes (IDs & Usage)

- Tool call IDs must be unique across steps. Some providers reuse simple IDs like `call_1` for every step. The SDK’s streaming JSON now scopes tool IDs by step to prevent UI deduping, and the Gemini adapter prefixes IDs with a per-stream nonce to avoid cross-step collisions. If you write your own provider, ensure non-empty, stable IDs for tool calls and return the same ID on the corresponding tool result.
- Streaming providers should include token usage in the final `finish` event so runners and UIs can report accurate totals. The Gemini provider now propagates usage from streamed chunks into the `finish` event.

## Observability & Monitoring

### OpenTelemetry Integration

Set up comprehensive observability:

```go
import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/shillcollin/gai/core"
    "github.com/shillcollin/gai/obs"
    "go.opentelemetry.io/otel/attribute"
)

func setupObservability(ctx context.Context) (func(context.Context) error, error) {
    opts := obs.DefaultOptions()
    opts.ServiceName = os.Getenv("OTEL_SERVICE_NAME")
    if opts.ServiceName == "" {
        opts.ServiceName = "ai-service"
    }
    opts.Environment = os.Getenv("GAI_ENV")
    opts.Exporter = obs.ExporterOTLP
    opts.Endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    opts.Insecure = os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true"

    if key := os.Getenv("BRAINTRUST_API_KEY"); key != "" {
        opts.Braintrust = obs.BraintrustOptions{
            Enabled:   true,
            APIKey:    key,
            Project:   os.Getenv("BRAINTRUST_PROJECT_NAME"),
            ProjectID: os.Getenv("BRAINTRUST_PROJECT_ID"),
            Dataset:   os.Getenv("BRAINTRUST_DATASET"),
        }
    }

    shutdown, err := obs.Init(ctx, opts)
    if err != nil {
        return nil, fmt.Errorf("failed to init observability: %w", err)
    }
    return shutdown, nil
}

func tracedOperation(ctx context.Context, p core.Provider, req core.Request) (_ *core.TextResult, err error) {
    ctx, recorder := obs.StartRequest(ctx, "business_logic",
        attribute.String("feature", "chat"),
        attribute.String("ai.provider", p.Capabilities().Provider),
    )
    var usage obs.UsageTokens
    defer func() {
        recorder.End(err, usage)
    }()

    res, err := p.GenerateText(ctx, req)
    if err != nil {
        return nil, err
    }

    usage = obs.UsageFromCore(res.Usage)
    return res, nil
}

func logCompletion(ctx context.Context, res *core.TextResult, history []core.Message, requestID string) {
    obs.LogCompletion(ctx, obs.Completion{
        Provider:     res.Provider,
        Model:        res.Model,
        RequestID:    requestID,
        Input:        obs.MessagesFromCore(history),
        Output:       obs.MessageFromCore(core.AssistantMessage(res.Text)),
        Usage:        obs.UsageFromCore(res.Usage),
        LatencyMS:    res.LatencyMS,
        ToolCalls:    obs.ToolCallsFromSteps(res.Steps),
        CreatedAtUTC: time.Now().UTC().UnixMilli(),
    })
}
```

`ToolCalls` captures each tool run, including provider metadata such as Gemini thought signatures that must be echoed on the next turn.

Braintrust is the default evaluation sink. Provide `BRAINTRUST_API_KEY`, `BRAINTRUST_PROJECT_NAME` (or `BRAINTRUST_PROJECT_ID`), and optional `BRAINTRUST_DATASET` to stream normalized completions automatically. The `obs` package also exposes an `ArizeOptions` struct so future releases can wire Arize exports without touching call sites; set `ARIZE_ENABLED=true` to opt-in once support lands.

### Metrics and Dashboards

Key metrics automatically collected:

```go
// Automatic metrics collected by the SDK:
// - gai.requests (by provider/model)
// - gai.request.latency_ms (histogram)
// - gai.tokens.input (histogram)
// - gai.tokens.output (histogram)
// - gai.tokens.total (histogram)
// - gai.tool.count (by name, status)
// - gai.tool.duration_ms (histogram)
// - gai.stream.events (by type)
// - gai.stream.drops (should be zero)

// Custom metrics
func recordCustomMetrics(result *core.TextResult) {
    // Use your metrics library
    metrics.Counter("feature.usage", 1, map[string]string{
        "feature": "summarization",
        "model":   result.Model,
    })

    metrics.Histogram("response.latency", result.LatencyMS, nil)
    metrics.Gauge("concurrent.requests", getCurrentRequests(), nil)
}
```

### Evaluation Framework

Run systematic evaluations:

```go
import "github.com/shillcollin/gai/evals"

func runEvaluations(ctx context.Context, p core.Provider) error {
    // Define evaluation suite
    suite := evals.NewSuite(
        evals.WithTask("summarization", evals.Task{
            Input: func(idx int) core.Request {
                return core.Request{
                    Messages: []core.Message{
                        {Role: core.System, Parts: []core.Part{
                            core.Text{Text: "Summarize the following text"},
                        }},
                        {Role: core.User, Parts: []core.Part{
                            core.Text{Text: testDocuments[idx]},
                        }},
                    },
                    MaxTokens: 200,
                }
            },
            Judge: func(ctx context.Context, result core.TextResult) (evals.Scores, error) {
                scores := evals.Scores{}

                // Length score
                targetLength := 150
                actualLength := len(strings.Fields(result.Text))
                scores["length_accuracy"] = 1.0 - math.Abs(float64(actualLength-targetLength))/float64(targetLength)

                // Readability score (simplified)
                sentences := strings.Count(result.Text, ".") + strings.Count(result.Text, "!") + strings.Count(result.Text, "?")
                wordsPerSentence := float64(actualLength) / float64(max(sentences, 1))
                scores["readability"] = min(1.0, 20.0/wordsPerSentence)

                // LLM-based evaluation
                judgeResult, err := p.GenerateText(ctx, core.Request{
                    Messages: []core.Message{
                        {Role: core.System, Parts: []core.Part{
                            core.Text{Text: "Rate this summary from 0 to 1 for completeness and accuracy"},
                        }},
                        {Role: core.User, Parts: []core.Part{
                            core.Text{Text: fmt.Sprintf("Original: %s\n\nSummary: %s",
                                testDocuments[idx], result.Text)},
                        }},
                    },
                    Temperature: 0,
                })

                if err == nil {
                    // Parse score from response
                    scores["llm_quality"] = parseScore(judgeResult.Text)
                }

                return scores, nil
            },
        }),
    )

    // Run evaluations
    results, err := suite.Run(ctx, p,
        evals.WithSamples(100),
        evals.WithParallel(4),
        evals.WithExport("braintrust"),
        evals.WithTags(map[string]string{
            "version": "v1.2.3",
            "experiment": "baseline",
        }),
    )

    if err != nil {
        return fmt.Errorf("evaluation failed: %w", err)
    }

    // Analyze results
    for task, scores := range results {
        fmt.Printf("Task: %s\n", task)
        for metric, value := range scores {
            fmt.Printf("  %s: %.3f\n", metric, value)
        }
    }

    return nil
}
```

## Error Handling & Retries

### Error Taxonomy

Understanding and handling different error types:

```go
import "github.com/shillcollin/gai/core/aierrors"

func handleErrors(err error) {
    if err == nil {
        return
    }

    switch {
    case aierrors.IsRateLimited(err):
        // Handle rate limiting with exponential backoff
        fmt.Println("Rate limited, backing off...")
        time.Sleep(calculateBackoff())

    case aierrors.IsContentFiltered(err):
        // Content was blocked by safety filters
        fmt.Println("Content filtered, adjusting prompt...")

    case aierrors.IsTransient(err):
        // Temporary error, safe to retry
        fmt.Println("Transient error, retrying...")

    case aierrors.IsBadRequest(err):
        // Invalid request, don't retry
        fmt.Println("Bad request, check parameters")

    case aierrors.IsTimeout(err):
        // Request timed out
        fmt.Println("Timeout, considering retry with longer timeout")

    case aierrors.IsCanceled(err):
        // Context was canceled
        fmt.Println("Operation canceled")

    case aierrors.IsToolError(err):
        // Tool execution failed
        fmt.Println("Tool error:", err)

    default:
        // Unknown error
        fmt.Printf("Unexpected error: %v\n", err)
    }
}
```

### Retry Strategies

Implement intelligent retry logic:

```go
import (
    "github.com/shillcollin/gai/middleware"
)

func setupRetries(p core.Provider) core.Provider {
    // Wrap provider with retry middleware
    return middleware.WithRetry(p, middleware.RetryOpts{
        MaxAttempts: 4,
        BaseDelay:   100 * time.Millisecond,
        MaxDelay:    30 * time.Second,
        Multiplier:  2.0,
        Jitter:      true,

        // Custom retry decision
        ShouldRetry: func(err error, attempt int) bool {
            // Don't retry bad requests
            if aierrors.IsBadRequest(err) {
                return false
            }

            // Always retry rate limits and transient errors
            if aierrors.IsRateLimited(err) || aierrors.IsTransient(err) {
                return attempt < 4
            }

            // Retry timeouts once
            if aierrors.IsTimeout(err) {
                return attempt < 2
            }

            return false
        },

        // Custom backoff for rate limits
        CalculateDelay: func(err error, attempt int, baseDelay time.Duration) time.Duration {
            if aierrors.IsRateLimited(err) {
                // Check for Retry-After header
                if retryAfter := aierrors.GetRetryAfter(err); retryAfter > 0 {
                    return retryAfter
                }
                // Use exponential backoff for rate limits
                return time.Duration(math.Pow(2, float64(attempt))) * time.Second
            }

            // Default exponential backoff with jitter
            delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt-1)))
            if middleware.RetryOpts.Jitter {
                jitter := time.Duration(rand.Float64() * float64(delay) * 0.3)
                delay = delay + jitter
            }
            return min(delay, middleware.RetryOpts.MaxDelay)
        },
    })
}
```

### Parameter Policies & Warnings

Providers frequently tighten request contracts as new models ship. OpenAI’s GPT‑5 and `o`-series reasoning models require `max_completion_tokens` and ignore custom temperatures, while GPT-4.1 keeps the classic knobs.citeturn0search1turn0search3turn0search10 Anthropic’s Claude Sonnet 4/Opus 4.1 still use `max_tokens` but publish hard output caps per release.citeturn1search0

The SDK normalizes these quirks automatically. When a request includes a parameter that must be renamed or dropped, the provider adapter adjusts the payload and emits a non-fatal `core.Warning` so you can observe what changed:

```go
resp, err := openaiClient.GenerateText(ctx, core.Request{
    Model:     "gpt-5-nano",
    MaxTokens: 800,
    Temperature: 0.2,
})
if err != nil {
    return err
}

for _, warn := range resp.Warnings {
    log.Printf("warning field=%s code=%s msg=%s", warn.Field, warn.Code, warn.Message)
}

stream, err := openaiClient.StreamText(ctx, core.Request{Model: "o4-mini", MaxTokens: 600})
if err != nil {
    return err
}
defer stream.Close()
if ws := stream.Warnings(); len(ws) > 0 {
    metrics.RecordWarnings(ws)
}
```

These warnings keep telemetry honest (they flow into Braintrust/OTel spans) and make it easy to adapt front-end controls—e.g., disable the temperature slider when `stream.Warnings()` reports it was dropped for a reasoning model.

### Circuit Breaker Pattern

Prevent cascading failures:

```go
type CircuitBreaker struct {
    provider        core.Provider
    failureThreshold int
    resetTimeout    time.Duration

    mu              sync.Mutex
    failures        int
    lastFailure     time.Time
    state           string // "closed", "open", "half-open"
}

func NewCircuitBreaker(p core.Provider) *CircuitBreaker {
    return &CircuitBreaker{
        provider:         p,
        failureThreshold: 5,
        resetTimeout:     30 * time.Second,
        state:           "closed",
    }
}

func (cb *CircuitBreaker) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
    cb.mu.Lock()

    // Check circuit state
    if cb.state == "open" {
        if time.Since(cb.lastFailure) > cb.resetTimeout {
            cb.state = "half-open"
            cb.failures = 0
        } else {
            cb.mu.Unlock()
            return nil, fmt.Errorf("circuit breaker open")
        }
    }
    cb.mu.Unlock()

    // Make the actual call
    result, err := cb.provider.GenerateText(ctx, req)

    cb.mu.Lock()
    defer cb.mu.Unlock()

    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()

        if cb.failures >= cb.failureThreshold {
            cb.state = "open"
            fmt.Printf("Circuit breaker opened after %d failures\n", cb.failures)
        }

        return nil, err
    }

    // Success - reset on half-open, reduce failures on closed
    if cb.state == "half-open" {
        cb.state = "closed"
        cb.failures = 0
        fmt.Println("Circuit breaker closed after successful call")
    } else if cb.failures > 0 {
        cb.failures--
    }

    return result, nil
}
```

## Performance Optimization

### HTTP Client Optimization

Configure optimal HTTP settings:

```go
func createOptimizedHTTPClient() *http.Client {
    return &http.Client{
        Timeout: 60 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 10,
            MaxConnsPerHost:     10,
            IdleConnTimeout:     90 * time.Second,
            DisableKeepAlives:   false, // Keep connections alive
            DisableCompression:  false, // Enable gzip
            ForceAttemptHTTP2:   true,  // Use HTTP/2 when available

            // Connection timeouts
            DialContext: (&net.Dialer{
                Timeout:   30 * time.Second,
                KeepAlive: 30 * time.Second,
            }).DialContext,

            TLSHandshakeTimeout:   10 * time.Second,
            ExpectContinueTimeout: 1 * time.Second,
        },
    }
}

// Use with provider
p := openai.New(
    openai.WithHTTPClient(createOptimizedHTTPClient()),
)
```

### Connection Pooling

Manage provider connections efficiently:

```go
type ProviderPool struct {
    providers []core.Provider
    index     uint32
}

func NewProviderPool(providers ...core.Provider) *ProviderPool {
    return &ProviderPool{
        providers: providers,
    }
}

func (pp *ProviderPool) Get() core.Provider {
    // Round-robin selection
    idx := atomic.AddUint32(&pp.index, 1) % uint32(len(pp.providers))
    return pp.providers[idx]
}

// Usage
pool := NewProviderPool(
    openai.New(openai.WithAPIKey(key1)),
    openai.New(openai.WithAPIKey(key2)),
    openai.New(openai.WithAPIKey(key3)),
)

// Distribute load across multiple API keys
provider := pool.Get()
result, _ := provider.GenerateText(ctx, req)
```

### Token Optimization

Manage token usage efficiently:

```go
func optimizeTokenUsage(req core.Request) core.Request {
    // Estimate tokens before sending
    estimated := core.EstimateTokens(req)

    // Truncate if necessary
    if estimated.Input > 120000 {
        // Implement sliding window or summarization
        req = truncateMessages(req, 100000)
    }

    // Adjust max tokens based on input
    if req.MaxTokens == 0 {
        remaining := 128000 - estimated.Input
        req.MaxTokens = min(4096, remaining/2)
    }

    return req
}

func truncateMessages(req core.Request, targetTokens int) core.Request {
    // Keep system message and most recent messages
    if len(req.Messages) <= 2 {
        return req
    }

    truncated := make([]core.Message, 0, len(req.Messages))

    // Always keep system message
    if req.Messages[0].Role == core.System {
        truncated = append(truncated, req.Messages[0])
    }

    // Add messages from the end until we hit the limit
    currentTokens := 0
    for i := len(req.Messages) - 1; i >= 1; i-- {
        msgTokens := estimateMessageTokens(req.Messages[i])
        if currentTokens+msgTokens > targetTokens {
            break
        }
        truncated = append([]core.Message{req.Messages[i]}, truncated...)
        currentTokens += msgTokens
    }

    req.Messages = truncated
    return req
}
```

### Streaming Buffer Management

Optimize streaming performance:

```go
func optimizedStreaming(ctx context.Context, p core.Provider) error {
    // Create stream with buffer hints
    stream, err := p.StreamText(ctx, core.Request{
        Messages: msgs,
        Stream:   true,
        ProviderOptions: map[string]any{
            "stream.buffer_size": 1024,     // Hint for internal buffers
            "stream.flush_interval": "50ms", // Flush frequency
        },
    })

    if err != nil {
        return err
    }
    defer stream.Close()

    // Use buffered writer for output
    writer := bufio.NewWriterSize(os.Stdout, 4096)
    defer writer.Flush()

    // Process with minimal allocations
    var totalText strings.Builder
    totalText.Grow(4096) // Pre-allocate

    for event := range stream.Events() {
        switch event.Type {
        case core.EventTextDelta:
            totalText.WriteString(event.TextDelta)
            writer.WriteString(event.TextDelta)

            // Flush periodically for responsiveness
            if totalText.Len()%100 == 0 {
                writer.Flush()
            }
        }
    }

    return nil
}
```

## Testing Strategies

### Unit Testing with Mocks

Test your code without calling real providers:

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)

// Mock provider for testing
type MockProvider struct {
    mock.Mock
}

func (m *MockProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
    args := m.Called(ctx, req)
    if result := args.Get(0); result != nil {
        return result.(*core.TextResult), args.Error(1)
    }
    return nil, args.Error(1)
}

func (m *MockProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
    args := m.Called(ctx, req)
    if stream := args.Get(0); stream != nil {
        return stream.(*core.Stream), args.Error(1)
    }
    return nil, args.Error(1)
}

func (m *MockProvider) Capabilities() core.Capabilities {
    return core.Capabilities{
        StrictJSON: true,
        ParallelToolCalls: true,
    }
}

// Test using the mock
func TestMyFunction(t *testing.T) {
    // Create mock provider
    mockProvider := new(MockProvider)

    // Set expectations
    expectedResult := &core.TextResult{
        Text: "Mocked response",
        Usage: core.Usage{
            InputTokens:  10,
            OutputTokens: 20,
            TotalTokens:  30,
        },
    }

    mockProvider.On("GenerateText", mock.Anything, mock.MatchedBy(func(req core.Request) bool {
        return len(req.Messages) > 0
    })).Return(expectedResult, nil)

    // Run test
    result, err := myFunction(context.Background(), mockProvider)

    // Assertions
    assert.NoError(t, err)
    assert.Equal(t, "Mocked response", result)

    // Verify expectations
    mockProvider.AssertExpectations(t)
}
```

### Contract Testing

Ensure providers meet interface contracts:

```go
func TestProviderContract(t *testing.T, p core.Provider) {
    ctx := context.Background()

    t.Run("GenerateText", func(t *testing.T) {
        result, err := p.GenerateText(ctx, core.Request{
            Messages: []core.Message{
                {Role: core.User, Parts: []core.Part{
                    core.Text{Text: "Say 'test'"},
                }},
            },
            MaxTokens:   10,
            Temperature: 0,
        })

        assert.NoError(t, err)
        assert.NotEmpty(t, result.Text)
        assert.Greater(t, result.Usage.TotalTokens, 0)
    })

    t.Run("StreamingIntegrity", func(t *testing.T) {
        stream, err := p.StreamText(ctx, core.Request{
            Messages: []core.Message{
                {Role: core.User, Parts: []core.Part{
                    core.Text{Text: "Count to 3"},
                }},
            },
            Stream:    true,
            MaxTokens: 50,
        })

        assert.NoError(t, err)
        defer stream.Close()

        var lastSeq int
        eventCount := 0
        finishCount := 0

        for event := range stream.Events() {
            eventCount++

            // Verify sequence increases
            assert.Greater(t, event.Seq, lastSeq)
            lastSeq = event.Seq

            // Count terminal events
            if event.Type == core.EventFinish || event.Type == core.EventError {
                finishCount++
            }
        }

        // Verify exactly one terminal event
        assert.Equal(t, 1, finishCount)
        assert.Greater(t, eventCount, 0)
    })
}
```

### Integration Testing

Test with real providers (gated by environment):

```go
//go:build integration

func TestLiveProviders(t *testing.T) {
    if os.Getenv("GAI_LIVE") != "1" {
        t.Skip("Skipping live tests (set GAI_LIVE=1 to run)")
    }

    providers := map[string]core.Provider{
        "openai": openai.New(
            openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
            openai.WithModel("gpt-4o-mini"),
        ),
        "anthropic": anthropic.New(
            anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
            anthropic.WithModel("claude-3-haiku"),
        ),
    }

    for name, provider := range providers {
        t.Run(name, func(t *testing.T) {
            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()

            result, err := provider.GenerateText(ctx, core.Request{
                Messages: []core.Message{
                    {Role: core.User, Parts: []core.Part{
                        core.Text{Text: "Say 'Hello from gai' and nothing else"},
                    }},
                },
                MaxTokens:   20,
                Temperature: 0,
            })

            assert.NoError(t, err)
            assert.Contains(t, result.Text, "Hello from gai")
            assert.Less(t, result.Usage.CostUSD, 0.01) // Cost sanity check
        })
    }
}
```

## Production Best Practices

### Configuration Management

Structure configuration for different environments:

```go
type Config struct {
    Environment string
    Providers   ProvidersConfig
    Observability ObsConfig
    Limits      LimitsConfig
    Features    FeatureFlags
}

type ProvidersConfig struct {
    Primary   ProviderConfig
    Fallback  []ProviderConfig
    Routing   RoutingConfig
}

type ProviderConfig struct {
    Type      string
    APIKey    string `sensitive:"true"`
    Model     string
    BaseURL   string
    MaxRetries int
    Timeout   time.Duration
}

type LimitsConfig struct {
    MaxTokensPerRequest   int
    MaxRequestsPerMinute  int
    MaxConcurrentRequests int
    BudgetUSDPerDay      float64
}

func LoadConfig(env string) (*Config, error) {
    // Load base configuration
    cfg := &Config{
        Environment: env,
    }

    // Load from environment variables
    if err := envconfig.Process("GAI", cfg); err != nil {
        return nil, err
    }

    // Apply environment-specific overrides
    switch env {
    case "production":
        cfg.Limits.MaxTokensPerRequest = 4000
        cfg.Observability.SampleRate = 0.1
        cfg.Providers.Primary.MaxRetries = 5

    case "staging":
        cfg.Limits.MaxTokensPerRequest = 2000
        cfg.Observability.SampleRate = 1.0
        cfg.Providers.Primary.MaxRetries = 3

    case "development":
        cfg.Limits.MaxTokensPerRequest = 500
        cfg.Observability.SampleRate = 1.0
        cfg.Providers.Primary.MaxRetries = 1
    }

    return cfg, nil
}
```

### Rate Limiting

Implement application-level rate limiting:

```go
import "golang.org/x/time/rate"

type RateLimitedProvider struct {
    provider core.Provider
    limiter  *rate.Limiter
    name     string
}

func NewRateLimitedProvider(p core.Provider, rps int, burst int) *RateLimitedProvider {
    return &RateLimitedProvider{
        provider: p,
        limiter:  rate.NewLimiter(rate.Limit(rps), burst),
        name:     fmt.Sprintf("rate_limited_%d_rps", rps),
    }
}

func (rlp *RateLimitedProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
    // Wait for rate limit
    if err := rlp.limiter.Wait(ctx); err != nil {
        return nil, fmt.Errorf("rate limit: %w", err)
    }

    return rlp.provider.GenerateText(ctx, req)
}

// Per-user rate limiting
type UserRateLimiter struct {
    mu       sync.RWMutex
    limiters map[string]*rate.Limiter
    rps      int
    burst    int
}

func (url *UserRateLimiter) Allow(userID string) bool {
    url.mu.RLock()
    limiter, exists := url.limiters[userID]
    url.mu.RUnlock()

    if !exists {
        url.mu.Lock()
        limiter = rate.NewLimiter(rate.Limit(url.rps), url.burst)
        url.limiters[userID] = limiter
        url.mu.Unlock()
    }

    return limiter.Allow()
}
```

### Cost Management

Track and control AI spending:

```go
type CostTracker struct {
    mu            sync.Mutex
    dailySpend    map[string]float64
    monthlySpend  float64
    limits        CostLimits
    alertFunc     func(string, float64)
}

type CostLimits struct {
    DailyUSD   float64
    MonthlyUSD float64
    PerUserUSD float64
}

func (ct *CostTracker) RecordUsage(userID string, usage core.Usage) error {
    ct.mu.Lock()
    defer ct.mu.Unlock()

    today := time.Now().Format("2006-01-02")

    // Update daily spend
    if ct.dailySpend[today] == 0 {
        ct.dailySpend = map[string]float64{} // Reset daily map
    }
    ct.dailySpend[today] += usage.CostUSD

    // Update monthly spend
    ct.monthlySpend += usage.CostUSD

    // Check limits
    if ct.dailySpend[today] > ct.limits.DailyUSD {
        ct.alertFunc("daily_limit_exceeded", ct.dailySpend[today])
        return fmt.Errorf("daily cost limit exceeded: $%.2f", ct.dailySpend[today])
    }

    if ct.monthlySpend > ct.limits.MonthlyUSD {
        ct.alertFunc("monthly_limit_exceeded", ct.monthlySpend)
        return fmt.Errorf("monthly cost limit exceeded: $%.2f", ct.monthlySpend)
    }

    return nil
}

// Middleware to track costs
func withCostTracking(tracker *CostTracker) func(core.Provider) core.Provider {
    return func(p core.Provider) core.Provider {
        return &costTrackingProvider{
            Provider: p,
            tracker:  tracker,
        }
    }
}
```

### Graceful Degradation

Handle failures with fallback strategies:

```go
type FallbackProvider struct {
    primary   core.Provider
    fallbacks []core.Provider
    strategy  FallbackStrategy
}

type FallbackStrategy interface {
    ShouldFallback(error) bool
    SelectFallback([]core.Provider, error) core.Provider
}

func (fp *FallbackProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
    // Try primary provider
    result, err := fp.primary.GenerateText(ctx, req)
    if err == nil {
        return result, nil
    }

    // Check if we should fallback
    if !fp.strategy.ShouldFallback(err) {
        return nil, err
    }

    // Try fallback providers
    fallback := fp.strategy.SelectFallback(fp.fallbacks, err)
    if fallback == nil {
        return nil, fmt.Errorf("all providers failed: %w", err)
    }

    // Potentially adjust request for fallback provider
    fallbackReq := adjustRequestForFallback(req, fallback)

    return fallback.GenerateText(ctx, fallbackReq)
}

func adjustRequestForFallback(req core.Request, provider core.Provider) core.Request {
    // Adjust model name if needed
    caps := provider.Capabilities()

    // Remove unsupported features
    if !caps.ParallelToolCalls && len(req.Tools) > 1 {
        // Limit to sequential tool calls
        req.ProviderOptions["disable_parallel_tools"] = true
    }

    // Adjust token limits
    // Some fallback providers might have different limits

    return req
}
```

## Advanced Patterns

### Memory Management

Implement conversation memory with context windows:

```go
type ConversationMemory struct {
    messages      []core.Message
    maxMessages   int
    maxTokens     int
    summarizer    core.Provider
    summaryPrompt string
}

func NewConversationMemory(maxMessages, maxTokens int) *ConversationMemory {
    return &ConversationMemory{
        maxMessages: maxMessages,
        maxTokens:   maxTokens,
    }
}

func (cm *ConversationMemory) Add(msg core.Message) {
    cm.messages = append(cm.messages, msg)
    cm.compact()
}

func (cm *ConversationMemory) compact() {
    // Keep system message + recent messages
    if len(cm.messages) <= cm.maxMessages {
        return
    }

    // Find system message
    systemIdx := -1
    for i, msg := range cm.messages {
        if msg.Role == core.System {
            systemIdx = i
            break
        }
    }

    // Calculate messages to keep
    keepStart := len(cm.messages) - cm.maxMessages + 1
    if systemIdx >= 0 {
        keepStart++ // Account for system message
    }

    // Build compacted message list
    compacted := make([]core.Message, 0, cm.maxMessages)
    if systemIdx >= 0 {
        compacted = append(compacted, cm.messages[systemIdx])
    }

    // Summarize messages being removed
    if cm.summarizer != nil && keepStart > 1 {
        summary := cm.summarizeMessages(cm.messages[1:keepStart])
        if summary != "" {
            compacted = append(compacted, core.Message{
                Role: core.Assistant,
                Parts: []core.Part{
                    core.Text{Text: fmt.Sprintf("[Previous conversation summary: %s]", summary)},
                },
            })
        }
    }

    // Add recent messages
    compacted = append(compacted, cm.messages[keepStart:]...)
    cm.messages = compacted
}

func (cm *ConversationMemory) summarizeMessages(msgs []core.Message) string {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    result, err := cm.summarizer.GenerateText(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.System, Parts: []core.Part{
                core.Text{Text: cm.summaryPrompt},
            }},
            {Role: core.User, Parts: []core.Part{
                core.Text{Text: formatMessages(msgs)},
            }},
        },
        MaxTokens:   200,
        Temperature: 0.3,
    })

    if err != nil {
        return ""
    }

    return result.Text
}
```

### Semantic Caching

Cache responses based on semantic similarity:

```go
type SemanticCache struct {
    embedder    Embedder
    store       VectorStore
    threshold   float64
    ttl         time.Duration
}

type CachedResponse struct {
    Request   core.Request
    Response  core.TextResult
    Embedding []float32
    Timestamp time.Time
}

func (sc *SemanticCache) Get(ctx context.Context, req core.Request) (*core.TextResult, bool) {
    // Generate embedding for request
    embedding, err := sc.embedder.Embed(ctx, formatRequest(req))
    if err != nil {
        return nil, false
    }

    // Search for similar requests
    results, err := sc.store.Search(ctx, embedding, 1, sc.threshold)
    if err != nil || len(results) == 0 {
        return nil, false
    }

    cached := results[0].(*CachedResponse)

    // Check TTL
    if time.Since(cached.Timestamp) > sc.ttl {
        return nil, false
    }

    // Return cached response
    return &cached.Response, true
}

func (sc *SemanticCache) Set(ctx context.Context, req core.Request, resp core.TextResult) error {
    embedding, err := sc.embedder.Embed(ctx, formatRequest(req))
    if err != nil {
        return err
    }

    return sc.store.Insert(ctx, &CachedResponse{
        Request:   req,
        Response:  resp,
        Embedding: embedding,
        Timestamp: time.Now(),
    })
}
```

### Prompt Chaining

Orchestrate complex multi-step operations:

```go
type PromptChain struct {
    steps []ChainStep
}

type ChainStep struct {
    Name      string
    Prompt    func(prev StepResult) core.Request
    Processor func(core.TextResult) (StepResult, error)
    Optional  bool
}

type StepResult struct {
    Name   string
    Output any
    Raw    core.TextResult
}

func (pc *PromptChain) Execute(ctx context.Context, p core.Provider, input any) ([]StepResult, error) {
    results := make([]StepResult, 0, len(pc.steps))

    var prev StepResult
    if input != nil {
        prev = StepResult{Name: "input", Output: input}
    }

    for _, step := range pc.steps {
        // Build request from previous result
        req := step.Prompt(prev)

        // Execute request
        result, err := p.GenerateText(ctx, req)
        if err != nil {
            if step.Optional {
                continue
            }
            return results, fmt.Errorf("step %s failed: %w", step.Name, err)
        }

        // Process result
        stepResult, err := step.Processor(result)
        if err != nil {
            if step.Optional {
                continue
            }
            return results, fmt.Errorf("processing step %s failed: %w", step.Name, err)
        }

        stepResult.Name = step.Name
        stepResult.Raw = result
        results = append(results, stepResult)
        prev = stepResult
    }

    return results, nil
}

// Example chain for document processing
var documentChain = &PromptChain{
    steps: []ChainStep{
        {
            Name: "extract_entities",
            Prompt: func(prev StepResult) core.Request {
                return core.Request{
                    Messages: []core.Message{
                        {Role: core.System, Parts: []core.Part{
                            core.Text{Text: "Extract all named entities from the text"},
                        }},
                        {Role: core.User, Parts: []core.Part{
                            core.Text{Text: prev.Output.(string)},
                        }},
                    },
                }
            },
            Processor: func(result core.TextResult) (StepResult, error) {
                // Parse entities from response
                return StepResult{Output: parseEntities(result.Text)}, nil
            },
        },
        {
            Name: "classify_sentiment",
            Prompt: func(prev StepResult) core.Request {
                return core.Request{
                    Messages: []core.Message{
                        {Role: core.System, Parts: []core.Part{
                            core.Text{Text: "Classify the sentiment"},
                        }},
                        {Role: core.User, Parts: []core.Part{
                            core.Text{Text: prev.Raw.Text},
                        }},
                    },
                }
            },
            Processor: func(result core.TextResult) (StepResult, error) {
                return StepResult{Output: parseSentiment(result.Text)}, nil
            },
        },
    },
}
```

## Migration Guide

### Migrating from OpenAI SDK

```go
// Before: OpenAI SDK
import "github.com/openai/openai-go"

client := openai.NewClient(apiKey)
completion, err := client.Chat.Completions.Create(ctx, openai.ChatCompletionRequest{
    Model: "gpt-4",
    Messages: []openai.ChatMessage{
        {Role: "user", Content: "Hello"},
    },
})

// After: gai
import "github.com/shillcollin/gai/providers/openai"

p := openai.New(openai.WithAPIKey(apiKey))
result, err := p.GenerateText(ctx, core.Request{
    Model: "gpt-4",
    Messages: []core.Message{
        {Role: core.User, Parts: []core.Part{core.Text{Text: "Hello"}}},
    },
})
```

### Migrating from Anthropic SDK

```go
// Before: Anthropic SDK
import "github.com/anthropic/anthropic-go"

client := anthropic.NewClient(apiKey)
response, err := client.Messages.Create(ctx, anthropic.MessageRequest{
    Model: "claude-3-sonnet",
    Messages: []anthropic.Message{
        {Role: "user", Content: "Hello"},
    },
})

// After: gai
import "github.com/shillcollin/gai/providers/anthropic"

p := anthropic.New(anthropic.WithAPIKey(apiKey))
result, err := p.GenerateText(ctx, core.Request{
    Model: "claude-3-sonnet",
    Messages: []core.Message{
        {Role: core.User, Parts: []core.Part{core.Text{Text: "Hello"}}},
    },
})
```

### Provider-Agnostic Migration

Make your code provider-agnostic:

```go
// Define interface your app needs
type AIService interface {
    Generate(ctx context.Context, prompt string) (string, error)
    Stream(ctx context.Context, prompt string, callback func(string)) error
}

// Implement with gai
type GaiService struct {
    provider core.Provider
}

func NewGaiService(providerName string) (*GaiService, error) {
    var provider core.Provider

    switch providerName {
    case "openai":
        provider = openai.New(openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")))
    case "anthropic":
        provider = anthropic.New(anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
    case "gemini":
        provider = gemini.New(gemini.WithAPIKey(os.Getenv("GOOGLE_API_KEY")))
    default:
        return nil, fmt.Errorf("unknown provider: %s", providerName)
    }

    return &GaiService{provider: provider}, nil
}

func (s *GaiService) Generate(ctx context.Context, prompt string) (string, error) {
    result, err := s.provider.GenerateText(ctx, core.Request{
        Messages: []core.Message{
            {Role: core.User, Parts: []core.Part{core.Text{Text: prompt}}},
        },
    })
    if err != nil {
        return "", err
    }
    return result.Text, nil
}

// Now switching providers requires no code changes
service, _ := NewGaiService("openai")   // or "anthropic", "gemini"
response, _ := service.Generate(ctx, "Hello")
```

## Conclusion

The gai SDK provides a comprehensive, production-ready foundation for building AI applications in Go. Its provider-agnostic design, strong typing, built-in observability, and extensive feature set make it the ideal choice for teams building serious AI applications.

Key takeaways:
- **Provider Independence**: Switch between providers without changing application logic
- **Type Safety**: Leverage Go's type system for structured outputs and tools
- **Production Ready**: Built-in observability, error handling, and retry mechanisms
- **Performance**: Optimized for streaming, concurrency, and efficient resource usage
- **Extensible**: Clean interfaces allow custom providers, middleware, and tools

For additional resources:
- [API Reference](API_REFERENCE.md) - Complete type and interface documentation
- [Examples](https://github.com/shillcollin/gai/tree/main/examples) - Runnable code examples
- [Provider Guides](PROVIDERS.md) - Provider-specific features and configuration
- [Streaming Specification](STREAMING.md) - Normalized event stream details

Start building with gai today and experience the difference a well-designed SDK makes in your AI applications.

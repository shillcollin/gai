# GAI v2.0 Upgrade Specification

This document captures the design decisions for the next major version of GAI, transforming it from a low-level provider SDK into a unified, developer-friendly AI toolkit for Go.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Current State Analysis](#2-current-state-analysis)
3. [Design Principles](#3-design-principles)
4. [Unified Client Architecture](#4-unified-client-architecture)
5. [Model String Format](#5-model-string-format)
6. [Request Builder API](#6-request-builder-api)
7. [Multimodal Results](#7-multimodal-results)
8. [Execution Methods](#8-execution-methods)
9. [Streaming API](#9-streaming-api)
10. [Voice & Audio Support](#10-voice--audio-support)
11. [Conversation Management](#11-conversation-management)
12. [Aliases & Routing](#12-aliases--routing)
13. [Configuration](#13-configuration)
14. [Provider Implementation](#14-provider-implementation)
15. [Package Structure](#15-package-structure)
16. [Migration Guide](#16-migration-guide)
17. [API Reference](#17-api-reference)
18. [Implementation Phases](#18-implementation-phases)

---

## 1. Executive Summary

### Problem

The current GAI SDK requires:
- Separate imports for each provider
- Provider-specific constructors and options
- Manual provider switching with code changes
- Separate `Runner` for tool execution
- No voice/audio support

### Solution

GAI v2.0 introduces:
- **Unified Client**: Single import, single client, all providers
- **Model Strings**: `provider/model` format for easy switching
- **Request Builder**: Fluent API for all request types
- **Multimodal Results**: Text, images, audio, tool calls in one response
- **Integrated Voice**: STT/TTS as first-class citizens
- **Conversation Object**: Automatic history management

### Before & After

```go
// BEFORE: v1.0
import "github.com/shillcollin/gai/providers/openai"
import "github.com/shillcollin/gai/providers/anthropic"
import "github.com/shillcollin/gai/runner"

openaiClient := openai.New(openai.WithAPIKey(key), openai.WithModel("gpt-5"))
r := runner.New(openaiClient)
result, err := r.ExecuteRequest(ctx, core.Request{
    Messages: []core.Message{core.UserMessage(core.TextPart("Hello"))},
    Tools:    tools,
})

// AFTER: v2.0
import "github.com/shillcollin/gai"

client := gai.NewClient()  // auto-configures from env
result, err := client.Run(ctx,
    gai.Request("openai/gpt-5").
        User("Hello").
        Tools(tools...))
```

---

## 2. Current State Analysis

### What We Have (v1.0)

| Component | Status | Notes |
|-----------|--------|-------|
| `core/` | Stable | Provider interface, messages, parts, streaming |
| `providers/openai` | Stable | Chat Completions API |
| `providers/openai-responses` | Stable | Responses API |
| `providers/anthropic` | Stable | Messages API |
| `providers/gemini` | Stable | Generative AI API |
| `providers/groq` | Stable | OpenAI-compatible |
| `providers/xai` | Stable | OpenAI-compatible |
| `providers/compat` | Stable | Generic OpenAI-compatible |
| `runner/` | Stable | Multi-step tool orchestration |
| `tools/` | Stable | Type-safe tool framework |
| `stream/` | Stable | SSE/NDJSON helpers |
| `obs/` | Stable | OpenTelemetry integration |
| `prompts/` | Stable | Versioned prompt registry |

### Pain Points

1. **Import friction**: Changing providers requires import changes
2. **Constructor friction**: Each provider has different `New()` signatures
3. **Runner separation**: Tools require instantiating a separate Runner
4. **No voice support**: STT/TTS requires external integration
5. **No conversation management**: History tracking is manual
6. **No multimodal output handling**: Image/audio outputs not unified

---

## 3. Design Principles

### 3.1 Import What You Use

Providers self-register via blank imports. Only imported providers are compiled into your binary.

```go
import (
    "github.com/shillcollin/gai"

    // Import only the providers you need
    _ "github.com/shillcollin/gai/providers/openai"
    _ "github.com/shillcollin/gai/providers/anthropic"
)

client := gai.NewClient()
result, _ := client.Text(ctx, gai.Request("openai/gpt-5").User("Hello"))
```

This follows Go idioms (like `database/sql` drivers) and keeps binaries small.

### 3.2 Zero-Config by Default

```go
// This should "just work" if OPENAI_API_KEY is set
client := gai.NewClient()
result, _ := client.Text(ctx, gai.Request("openai/gpt-5").User("Hello"))
```

### 3.3 Explicit When Needed

```go
// Full control available when required
client := gai.NewClient(
    gai.WithAPIKey("openai", customKey),
    gai.WithBaseURL("openai", "https://custom.endpoint.com"),
    gai.WithHTTPClient(customHTTPClient),
)
```

### 3.4 Model String is the Switch

```go
// Changing providers = changing one string
result, _ := client.Text(ctx, gai.Request("openai/gpt-5").User("Hello"))
result, _ := client.Text(ctx, gai.Request("anthropic/claude-4-5-sonnet").User("Hello"))
result, _ := client.Text(ctx, gai.Request("gemini/gemini-3.0-flash").User("Hello"))
```

### 3.5 Voice is a Modality, Not a Mode

```go
// Same conversation API, voice is just input/output options
conv.Say(ctx, "Hello")                                    // text in, text out
conv.Say(ctx, gai.Audio(bytes))                          // audio in, text out
conv.Say(ctx, "Hello", gai.Voice("elevenlabs/rachel"))   // text in, audio+text out
conv.Say(ctx, gai.Audio(bytes), gai.Voice("..."))        // audio in, audio+text out
```

### 3.6 History is Always Text

```go
// Regardless of input modality, conversation history is text
messages := conv.Messages()
// All messages are text - audio inputs are transcribed, stored as text
```

### 3.7 Progressive Disclosure

| Level | API | Use Case |
|-------|-----|----------|
| Simple | `client.Text(ctx, req)` | Quick text generation |
| Standard | `client.Generate(ctx, req)` | Full multimodal access |
| Advanced | `client.Run(ctx, req)` | Agentic loops with tools |
| Expert | `providers/openai.New(...)` | Provider-specific features |

---

## 4. Unified Client Architecture

### 4.1 Client Structure

```go
type Client struct {
    providers   map[string]Provider      // "openai" -> Provider
    sttProviders map[string]STTProvider  // "deepgram" -> STTProvider
    ttsProviders map[string]TTSProvider  // "elevenlabs" -> TTSProvider
    aliases     map[string]string        // "fast" -> "groq/llama3-70b"
    voiceAliases map[string]VoiceConfig  // "support" -> {stt, llm, tts}
    defaults    ClientDefaults
    httpClient  *http.Client
    obs         *obs.Observer
}

type ClientDefaults struct {
    Model       string  // default model if none specified
    Voice       string  // default TTS voice
    STT         string  // default STT provider
    Temperature *float64
    MaxTokens   *int
}
```

### 4.2 Client Construction

```go
// Auto-configuration from environment
client := gai.NewClient()

// Explicit configuration
client := gai.NewClient(
    // API Keys
    gai.WithAPIKey("openai", os.Getenv("OPENAI_API_KEY")),
    gai.WithAPIKey("anthropic", os.Getenv("ANTHROPIC_API_KEY")),
    gai.WithAPIKey("deepgram", os.Getenv("DEEPGRAM_API_KEY")),
    gai.WithAPIKey("elevenlabs", os.Getenv("ELEVENLABS_API_KEY")),

    // Defaults
    gai.WithDefaultModel("anthropic/claude-4-5-sonnet"),
    gai.WithDefaultVoice("elevenlabs/rachel"),
    gai.WithDefaultSTT("deepgram/nova-2"),

    // Aliases
    gai.WithAlias("fast", "groq/llama3-70b-8192"),
    gai.WithAlias("smart", "anthropic/claude-4-5-sonnet"),
    gai.WithAlias("cheap", "openai/gpt-5-mini"),

    // Voice aliases (combined STT + LLM + TTS)
    gai.WithVoiceAlias("support", gai.VoiceConfig{
        STT: "deepgram/nova-2",
        LLM: "anthropic/claude-4-5-sonnet",
        TTS: "elevenlabs/rachel",
    }),

    // Advanced
    gai.WithHTTPClient(customClient),
    gai.WithObserver(obsConfig),
)
```

### 4.3 Environment Variables

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` | OpenAI authentication |
| `ANTHROPIC_API_KEY` | Anthropic authentication |
| `GOOGLE_API_KEY` | Google/Gemini authentication |
| `GROQ_API_KEY` | Groq authentication |
| `XAI_API_KEY` | xAI authentication |
| `DEEPGRAM_API_KEY` | Deepgram STT authentication |
| `ASSEMBLYAI_API_KEY` | AssemblyAI STT authentication |
| `ELEVENLABS_API_KEY` | ElevenLabs TTS authentication |
| `CARTESIA_API_KEY` | Cartesia TTS authentication |
| `PLAYHT_API_KEY` | PlayHT TTS authentication |
| `GAI_DEFAULT_MODEL` | Default model string |
| `GAI_DEFAULT_VOICE` | Default TTS voice |
| `GAI_DEFAULT_STT` | Default STT provider |

---

## 5. Model String Format

### 5.1 Format Specification

```
provider/model[:version]

Examples:
  openai/gpt-5
  openai/gpt-5:2024-08-06
  anthropic/claude-4-5-sonnet
  anthropic/claude-4-5-sonnet:20241022
  gemini/gemini-3.0-flash
  groq/llama3-70b-8192
  xai/grok-2
```

### 5.2 Provider Registry

| Provider ID | Provider Name | API Type |
|-------------|---------------|----------|
| `openai` | OpenAI | Chat Completions |
| `openai-responses` | OpenAI Responses | Responses API |
| `anthropic` | Anthropic | Messages API |
| `gemini` | Google Gemini | Generative AI |
| `groq` | Groq | OpenAI-compatible |
| `xai` | xAI | OpenAI-compatible |
| `deepgram` | Deepgram | STT |
| `assemblyai` | AssemblyAI | STT |
| `whisper` | OpenAI Whisper | STT |
| `elevenlabs` | ElevenLabs | TTS |
| `cartesia` | Cartesia | TTS |
| `playht` | PlayHT | TTS |

### 5.3 Model Resolution

```go
func (c *Client) resolveModel(model string) (provider Provider, modelID string, err error) {
    // Check aliases first
    if resolved, ok := c.aliases[model]; ok {
        model = resolved
    }

    // Parse provider/model format
    parts := strings.SplitN(model, "/", 2)
    if len(parts) != 2 {
        // No provider prefix - try to infer from model name
        provider, modelID = c.inferProvider(model)
        if provider == nil {
            return nil, "", fmt.Errorf("unknown model: %s (use provider/model format)", model)
        }
        return provider, modelID, nil
    }

    providerID, modelID := parts[0], parts[1]
    provider, ok := c.providers[providerID]
    if !ok {
        return nil, "", fmt.Errorf("unknown provider: %s", providerID)
    }

    return provider, modelID, nil
}
```

### 5.4 Voice String Format

```
provider/voice[:variant]

Examples:
  elevenlabs/rachel
  elevenlabs/adam
  cartesia/nova
  cartesia/sonic-english
  openai/alloy
  openai/shimmer
  playht/matthew
  deepgram/nova-2          (STT)
  assemblyai/best          (STT)
  whisper/large-v3         (STT)
```

---

## 6. Request Builder API

### 6.1 Request Type

```go
type Request struct {
    model           string
    messages        []Message
    tools           []ToolHandle
    toolChoice      ToolChoice
    outputSchema    any              // for structured output
    audioInput      []byte           // for voice input
    voiceOutput     string           // for voice output
    temperature     *float64
    maxTokens       *int
    topP            *float64
    topK            *int
    stopSequences   []string
    stopWhen        StopCondition    // for Run()
    maxSteps        int              // for Run()
    providerOptions map[string]any
    metadata        map[string]any
}
```

### 6.2 Fluent Builder

```go
// Constructor
func Request(model string) *RequestBuilder

// Message methods
func (r *RequestBuilder) System(content string) *RequestBuilder
func (r *RequestBuilder) User(content string) *RequestBuilder
func (r *RequestBuilder) Assistant(content string) *RequestBuilder
func (r *RequestBuilder) Messages(msgs ...Message) *RequestBuilder

// Multimodal input
func (r *RequestBuilder) Image(data []byte, mimeType string) *RequestBuilder
func (r *RequestBuilder) ImageURL(url string) *RequestBuilder
func (r *RequestBuilder) Audio(data []byte) *RequestBuilder  // triggers STT
func (r *RequestBuilder) File(data []byte, mimeType string) *RequestBuilder

// Tools
func (r *RequestBuilder) Tools(tools ...ToolHandle) *RequestBuilder
func (r *RequestBuilder) ToolChoice(choice ToolChoice) *RequestBuilder

// Output control
func (r *RequestBuilder) Schema(v any) *RequestBuilder           // structured output
func (r *RequestBuilder) Voice(voice string) *RequestBuilder     // TTS output
func (r *RequestBuilder) OutputTypes(types ...OutputType) *RequestBuilder

// Generation parameters
func (r *RequestBuilder) Temperature(t float64) *RequestBuilder
func (r *RequestBuilder) MaxTokens(n int) *RequestBuilder
func (r *RequestBuilder) TopP(p float64) *RequestBuilder
func (r *RequestBuilder) TopK(k int) *RequestBuilder
func (r *RequestBuilder) Stop(sequences ...string) *RequestBuilder

// Agentic control (for Run())
func (r *RequestBuilder) StopWhen(cond StopCondition) *RequestBuilder
func (r *RequestBuilder) MaxSteps(n int) *RequestBuilder
func (r *RequestBuilder) OnStop(finalizer Finalizer) *RequestBuilder

// Provider-specific
func (r *RequestBuilder) Option(key string, value any) *RequestBuilder
func (r *RequestBuilder) Metadata(key string, value any) *RequestBuilder

// Build
func (r *RequestBuilder) Build() Request
```

### 6.3 Usage Examples

```go
// Simple text request
req := gai.Request("openai/gpt-5").
    User("What is the capital of France?")

// With system prompt
req := gai.Request("anthropic/claude-4-5-sonnet").
    System("You are a helpful assistant").
    User("Explain quantum computing")

// Multi-turn conversation
req := gai.Request("openai/gpt-5").
    System("You are a math tutor").
    User("What is 2+2?").
    Assistant("2+2 equals 4").
    User("What about 3+3?")

// With image
req := gai.Request("openai/gpt-5").
    User("What's in this image?").
    ImageURL("https://example.com/image.png")

// With tools
req := gai.Request("anthropic/claude-4-5-sonnet").
    System("You have access to a calculator").
    User("What is 15% of 847?").
    Tools(calculatorTool)

// Structured output
req := gai.Request("openai/gpt-5").
    User("Give me a recipe for pasta").
    Schema(&Recipe{})

// Voice input/output
req := gai.Request("anthropic/claude-4-5-sonnet").
    Audio(userSpeechBytes).
    Voice("elevenlabs/rachel")

// Agentic loop
req := gai.Request("anthropic/claude-4-5-sonnet").
    System("You are a research assistant").
    User("Find and summarize the latest AI papers").
    Tools(searchTool, fetchTool).
    StopWhen(gai.NoMoreTools()).
    MaxSteps(10)
```

---

## 7. Multimodal Results

### 7.1 Result Type

```go
type Result struct {
    // Core content
    Parts       []Part          // all content parts in order

    // Convenience accessors populate from Parts
    text        string          // cached aggregated text
    images      []Image
    audio       []Audio
    toolCalls   []ToolCall

    // Voice-specific
    Transcript  string          // user's speech transcribed (if audio input)

    // Metadata
    Model       string
    Provider    string
    Usage       Usage
    FinishReason StopReason

    // For Run() - multi-step execution
    Steps       []Step

    // For conversation history
    newMessages []Message       // messages to append to history
}

// Part types (same as Message parts)
type Part interface { isPart() }

type Text struct { Text string }
type Image struct {
    Data     []byte
    MimeType string
    URL      string  // if generated with URL
}
type Audio struct {
    Data     []byte
    MimeType string
    Duration time.Duration
}
type ToolCall struct {
    ID    string
    Name  string
    Input map[string]any
}
```

### 7.2 Convenience Accessors

```go
// Text returns all text parts concatenated
func (r *Result) Text() string

// HasText returns true if result contains any text
func (r *Result) HasText() bool

// Image returns the first image or nil
func (r *Result) Image() *Image

// Images returns all images
func (r *Result) Images() []Image

// HasImage returns true if result contains any images
func (r *Result) HasImage() bool

// Audio returns the first audio or nil
func (r *Result) Audio() *Audio

// AudioData returns raw audio bytes of first audio, or nil
func (r *Result) AudioData() []byte

// HasAudio returns true if result contains any audio
func (r *Result) HasAudio() bool

// ToolCalls returns all tool calls
func (r *Result) ToolCalls() []ToolCall

// HasToolCalls returns true if result contains tool calls
func (r *Result) HasToolCalls() bool

// Messages returns new messages to append to conversation history
// For voice: includes transcribed user message + assistant response
func (r *Result) Messages() []Message
```

### 7.3 Usage Example

```go
result, err := client.Generate(ctx, req)
if err != nil {
    return err
}

// Check what we got
if result.HasText() {
    fmt.Println("Response:", result.Text())
}

if result.HasImage() {
    img := result.Image()
    os.WriteFile("output.png", img.Data, 0644)
}

if result.HasAudio() {
    speaker.Play(result.AudioData())
}

if result.HasToolCalls() {
    for _, call := range result.ToolCalls() {
        fmt.Printf("Tool: %s(%v)\n", call.Name, call.Input)
    }
}

// Update conversation history
history = append(history, result.Messages()...)
```

---

## 8. Execution Methods

### 8.1 Method Overview

| Method | Purpose | Returns |
|--------|---------|---------|
| `Generate(req)` | Universal - single LLM call | `*Result` |
| `Text(req)` | Convenience - text only | `string, error` |
| `Image(req)` | Convenience - image only | `*Image, error` |
| `Unmarshal(req, &v)` | Structured output | `error` |
| `Run(req)` | Agentic loop | `*Result` |
| `Stream(req)` | Streaming (all types) | `*Stream, error` |
| `StreamRun(req)` | Streaming agentic loop | `*Stream, error` |

### 8.2 Generate

The universal method - handles all cases, returns rich Result.

```go
func (c *Client) Generate(ctx context.Context, req *RequestBuilder) (*Result, error)

// Usage
result, err := client.Generate(ctx,
    gai.Request("openai/gpt-5").
        User("Hello"))

// Access any output type
fmt.Println(result.Text())
fmt.Println(result.Image())
fmt.Println(result.ToolCalls())
```

### 8.3 Text

Convenience for text-only responses. Errors if no text in response.

```go
func (c *Client) Text(ctx context.Context, req *RequestBuilder) (string, error)

// Usage
text, err := client.Text(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        User("Explain gravity"))
fmt.Println(text)
```

### 8.4 Image

Convenience for image generation. Errors if no image in response.

```go
func (c *Client) Image(ctx context.Context, req *RequestBuilder) (*Image, error)

// Usage
img, err := client.Image(ctx,
    gai.Request("openai/gpt-5").
        User("Generate an image of a sunset"))
os.WriteFile("sunset.png", img.Data, 0644)
```

### 8.5 Unmarshal

Structured output - parses response into typed struct.

```go
func (c *Client) Unmarshal(ctx context.Context, req *RequestBuilder, v any) error

// Usage
type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Steps       []string `json:"steps"`
}

var recipe Recipe
err := client.Unmarshal(ctx,
    gai.Request("openai/gpt-5").
        User("Give me a recipe for chocolate cake"),
    &recipe)

fmt.Println(recipe.Name)
```

### 8.6 Run

Agentic loop - executes until stop condition met.

```go
func (c *Client) Run(ctx context.Context, req *RequestBuilder) (*Result, error)

// Usage
result, err := client.Run(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        System("You are a research assistant").
        User("Find information about Mars rovers").
        Tools(searchTool, readTool).
        StopWhen(gai.NoMoreTools()).
        MaxSteps(10))

// Result contains all steps
for i, step := range result.Steps {
    fmt.Printf("Step %d: %s\n", i+1, step.Text)
    for _, tc := range step.ToolCalls {
        fmt.Printf("  Tool: %s\n", tc.Call.Name)
    }
}

fmt.Println("Final:", result.Text())
```

### 8.7 Stop Conditions

```go
// Built-in stop conditions
gai.NoMoreTools()                    // stop when no tool calls
gai.MaxSteps(n)                      // stop after n steps
gai.MaxTokens(n)                     // stop after n total tokens
gai.MaxCost(usd)                     // stop after cost threshold
gai.Custom(func(state) (bool, reason)) // custom condition

// Combinators
gai.Any(cond1, cond2)                // stop if any condition met
gai.All(cond1, cond2)                // stop if all conditions met
```

---

## 9. Streaming API

### 9.1 Stream Type

```go
type Stream struct {
    events   chan StreamEvent
    result   *Result
    err      error
    meta     StreamMeta
    warnings []Warning
}

func (s *Stream) Events() <-chan StreamEvent
func (s *Stream) Result() *Result      // available after stream ends
func (s *Stream) Err() error
func (s *Stream) Close() error
func (s *Stream) Meta() StreamMeta
func (s *Stream) Warnings() []Warning
```

### 9.2 Stream Events

```go
type EventType string

const (
    EventStart           EventType = "start"
    EventTextDelta       EventType = "text.delta"
    EventImageDelta      EventType = "image.delta"
    EventAudioDelta      EventType = "audio.delta"
    EventToolCall        EventType = "tool.call"
    EventToolResult      EventType = "tool.result"
    EventStepStart       EventType = "step.start"
    EventStepFinish      EventType = "step.finish"
    EventTranscript      EventType = "transcript"      // STT result
    EventReasoningDelta  EventType = "reasoning.delta"
    EventFinish          EventType = "finish"
    EventError           EventType = "error"
)

type StreamEvent struct {
    Type         EventType

    // Content deltas
    TextDelta    string
    ImageDelta   []byte
    AudioDelta   []byte

    // Voice
    Transcript   string      // user's transcribed speech

    // Tools
    ToolCall     ToolCall
    ToolResult   ToolResult

    // Reasoning (for models that support it)
    ReasoningDelta string

    // Metadata
    StepID       int
    Usage        Usage
    FinishReason *StopReason
    Error        error
}
```

### 9.3 Streaming Usage

```go
// Basic text streaming
stream, err := client.Stream(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        User("Tell me a story"))
if err != nil {
    return err
}
defer stream.Close()

for event := range stream.Events() {
    switch event.Type {
    case gai.EventTextDelta:
        fmt.Print(event.TextDelta)
    case gai.EventFinish:
        fmt.Println("\n\nDone!")
    case gai.EventError:
        return event.Error
    }
}

// Streaming with voice
stream, err := client.Stream(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Audio(userSpeechBytes).
        Voice("elevenlabs/rachel"))

for event := range stream.Events() {
    switch event.Type {
    case gai.EventTranscript:
        fmt.Println("User said:", event.Transcript)
    case gai.EventTextDelta:
        ui.AppendText(event.TextDelta)
    case gai.EventAudioDelta:
        speaker.Write(event.AudioDelta)
    }
}

// Streaming agentic loop
stream, err := client.StreamRun(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        User("Research AI news").
        Tools(searchTool).
        StopWhen(gai.NoMoreTools()))

for event := range stream.Events() {
    switch event.Type {
    case gai.EventStepStart:
        fmt.Printf("\n--- Step %d ---\n", event.StepID)
    case gai.EventTextDelta:
        fmt.Print(event.TextDelta)
    case gai.EventToolCall:
        fmt.Printf("\n[Calling %s]\n", event.ToolCall.Name)
    case gai.EventToolResult:
        fmt.Printf("[Result: %v]\n", event.ToolResult.Result)
    }
}
```

---

## 10. Voice & Audio Support

### 10.1 Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Voice Pipeline                        │
│                                                          │
│  Audio In ──► STT Provider ──► Text ──► LLM Provider    │
│                                          │               │
│                                          ▼               │
│                              Text ──► TTS Provider ──► Audio Out
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### 10.2 STT Providers

| Provider | Models | Notes |
|----------|--------|-------|
| `deepgram` | `nova-2`, `nova`, `enhanced`, `base` | Real-time, high accuracy |
| `assemblyai` | `best`, `nano` | Async, speaker diarization |
| `whisper` | `large-v3`, `medium`, `small`, `base` | OpenAI hosted |

### 10.3 TTS Providers

| Provider | Voices | Notes |
|----------|--------|-------|
| `elevenlabs` | `rachel`, `adam`, `antoni`, `bella`, ... | High quality, cloning |
| `cartesia` | `nova`, `sonic-english`, `sonic-multilingual` | Low latency |
| `playht` | `matthew`, `davis`, ... | Large voice library |
| `openai` | `alloy`, `echo`, `fable`, `onyx`, `nova`, `shimmer` | Built-in |

### 10.4 Low-Level STT/TTS

```go
// Standalone transcription
transcript, err := client.Transcribe(ctx, audioBytes)
transcript, err := client.Transcribe(ctx, audioBytes,
    gai.WithSTT("deepgram/nova-2"),
    gai.WithLanguage("en"))

// Standalone synthesis
audio, err := client.Synthesize(ctx, "Hello world")
audio, err := client.Synthesize(ctx, text,
    gai.WithVoice("elevenlabs/rachel"),
    gai.WithSpeed(1.1))

// Streaming synthesis
stream, err := client.StreamSynthesize(ctx, textChan,
    gai.WithVoice("cartesia/nova"))
for chunk := range stream.Audio() {
    speaker.Write(chunk)
}
```

### 10.5 Integrated Voice

```go
// Audio input (auto-transcribed)
result, err := client.Generate(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Messages(history...).
        Audio(userSpeechBytes))

result.Transcript  // what user said
result.Text()      // LLM response

// Audio output (auto-synthesized)
result, err := client.Generate(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Messages(history...).
        User("Hello").
        Voice("elevenlabs/rachel"))

result.Text()      // LLM response
result.Audio()     // synthesized audio

// Full voice pipeline
result, err := client.Generate(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Messages(history...).
        Audio(userSpeechBytes).
        Voice("elevenlabs/rachel"))

result.Transcript  // user speech → text
result.Text()      // LLM response
result.Audio()     // response → audio

// Streaming voice (low latency)
stream, err := client.Stream(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Audio(userSpeechBytes).
        Voice("elevenlabs/rachel"))

for event := range stream.Events() {
    switch event.Type {
    case gai.EventTranscript:
        fmt.Println("User:", event.Transcript)
    case gai.EventTextDelta:
        ui.AppendText(event.TextDelta)
    case gai.EventAudioDelta:
        speaker.Write(event.AudioDelta)  // plays as it generates
    }
}
```

### 10.6 Realtime APIs

For providers with native realtime support (OpenAI, Gemini):

```go
// Realtime session - bidirectional streaming
session, err := client.RealtimeSession(ctx, gai.RealtimeConfig{
    Model: "openai/gpt-5-realtime",
    Voice: "alloy",
    Tools: tools,
})
defer session.Close()

// Send audio continuously
go func() {
    for chunk := range microphone.Chunks() {
        session.SendAudio(chunk)
    }
}()

// Receive events
for event := range session.Events() {
    switch event.Type {
    case gai.EventTranscript:
        fmt.Println("User:", event.Transcript)
    case gai.EventAudioDelta:
        speaker.Write(event.AudioDelta)
    case gai.EventToolCall:
        result := executeTool(event.ToolCall)
        session.SendToolResult(result)
    case gai.EventInterrupt:
        speaker.Stop()  // user interrupted
    }
}
```

### 10.7 Smart Routing

Client automatically chooses best path:

```go
// Uses native realtime (OpenAI has it)
stream, _ := client.Stream(ctx,
    gai.Request("openai/gpt-5").
        Audio(bytes).
        Voice("alloy"))

// Uses STT → LLM → TTS pipeline (Anthropic doesn't have realtime)
stream, _ := client.Stream(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Audio(bytes).
        Voice("elevenlabs/rachel"))
```

---

## 11. Conversation Management

### 11.1 Conversation Type

```go
type Conversation struct {
    client     *Client
    messages   []Message
    model      string
    system     string
    voice      string
    tools      []ToolHandle
    metadata   map[string]any
}
```

### 11.2 Creating Conversations

```go
// Basic conversation
conv := client.Conversation(
    gai.Model("anthropic/claude-4-5-sonnet"),
)

// With options
conv := client.Conversation(
    gai.Model("anthropic/claude-4-5-sonnet"),
    gai.System("You are a helpful assistant"),
    gai.Voice("elevenlabs/rachel"),
    gai.Tools(searchTool, calcTool),
)

// From existing messages
conv := client.Conversation(
    gai.Model("anthropic/claude-4-5-sonnet"),
    gai.Messages(existingHistory...),
)
```

### 11.3 Conversation Methods

```go
// Single-turn methods
func (c *Conversation) Say(ctx context.Context, input any, opts ...Option) (*Result, error)
func (c *Conversation) Stream(ctx context.Context, input any, opts ...Option) (*Stream, error)

// Agentic methods
func (c *Conversation) Run(ctx context.Context, input any, opts ...Option) (*Result, error)
func (c *Conversation) StreamRun(ctx context.Context, input any, opts ...Option) (*Stream, error)

// History management
func (c *Conversation) Messages() []Message
func (c *Conversation) Clear()
func (c *Conversation) Rollback(n int)  // remove last n messages
func (c *Conversation) Fork() *Conversation  // create branch

// Serialization
func (c *Conversation) MarshalJSON() ([]byte, error)
func (c *Conversation) UnmarshalJSON(data []byte) error
```

### 11.4 Usage Examples

```go
conv := client.Conversation(
    gai.Model("anthropic/claude-4-5-sonnet"),
    gai.System("You are a helpful assistant"),
)

// Text turns
reply, _ := conv.Say(ctx, "Hello!")
fmt.Println(reply.Text())

reply, _ = conv.Say(ctx, "What's your name?")
fmt.Println(reply.Text())

// Voice turn
reply, _ = conv.Say(ctx, gai.Audio(userSpeech), gai.Voice("elevenlabs/rachel"))
fmt.Println("User said:", reply.Transcript)
fmt.Println("Assistant:", reply.Text())
speaker.Play(reply.AudioData())

// Streaming turn
stream, _ := conv.Stream(ctx, "Tell me a joke")
for event := range stream.Events() {
    if event.Type == gai.EventTextDelta {
        fmt.Print(event.TextDelta)
    }
}

// History is automatically maintained
messages := conv.Messages()
// [system: "You are a helpful assistant"]
// [user: "Hello!"]
// [assistant: "Hi there! How can I help you today?"]
// [user: "What's your name?"]
// [assistant: "I'm Claude, an AI assistant..."]
// [user: "..."] <- transcribed from audio
// [assistant: "..."]

// Serialize for persistence
data, _ := json.Marshal(conv)
os.WriteFile("conversation.json", data, 0644)

// Restore
data, _ = os.ReadFile("conversation.json")
conv2 := client.Conversation()
json.Unmarshal(data, conv2)
```

### 11.5 Chat App Example

```go
type ChatHandler struct {
    client *gai.Client
    conv   *gai.Conversation
}

func NewChatHandler(client *gai.Client) *ChatHandler {
    return &ChatHandler{
        client: client,
        conv: client.Conversation(
            gai.Model("anthropic/claude-4-5-sonnet"),
            gai.System("You are a helpful customer support agent"),
            gai.Voice("elevenlabs/rachel"),
        ),
    }
}

// Text message handler
func (h *ChatHandler) HandleText(ctx context.Context, text string) (*Response, error) {
    reply, err := h.conv.Say(ctx, text)
    if err != nil {
        return nil, err
    }
    return &Response{Text: reply.Text()}, nil
}

// Voice message handler
func (h *ChatHandler) HandleVoice(ctx context.Context, audio []byte, wantAudio bool) (*Response, error) {
    opts := []gai.Option{}
    if wantAudio {
        opts = append(opts, gai.Voice("elevenlabs/rachel"))
    }

    reply, err := h.conv.Say(ctx, gai.Audio(audio), opts...)
    if err != nil {
        return nil, err
    }

    return &Response{
        UserTranscript: reply.Transcript,
        Text:          reply.Text(),
        Audio:         reply.AudioData(),
    }, nil
}

// Streaming voice handler
func (h *ChatHandler) HandleVoiceStream(ctx context.Context, audio []byte, eventChan chan Event) error {
    stream, err := h.conv.Stream(ctx, gai.Audio(audio), gai.Voice("elevenlabs/rachel"))
    if err != nil {
        return err
    }
    defer stream.Close()

    for event := range stream.Events() {
        switch event.Type {
        case gai.EventTranscript:
            eventChan <- Event{Type: "transcript", Data: event.Transcript}
        case gai.EventTextDelta:
            eventChan <- Event{Type: "text", Data: event.TextDelta}
        case gai.EventAudioDelta:
            eventChan <- Event{Type: "audio", Data: event.AudioDelta}
        }
    }

    return stream.Err()
}

// Get history for display
func (h *ChatHandler) GetHistory() []gai.Message {
    return h.conv.Messages()
}
```

---

## 12. Aliases & Routing

### 12.1 Model Aliases

```go
client := gai.NewClient(
    gai.WithAlias("fast", "groq/llama3-70b-8192"),
    gai.WithAlias("smart", "anthropic/claude-4-5-sonnet"),
    gai.WithAlias("cheap", "openai/gpt-5-mini"),
    gai.WithAlias("vision", "openai/gpt-5"),
    gai.WithAlias("code", "anthropic/claude-4-5-sonnet"),
)

// Use aliases
result, _ := client.Text(ctx, gai.Request("fast").User("Quick question"))
result, _ := client.Text(ctx, gai.Request("smart").User("Complex analysis"))
```

### 12.2 Voice Aliases

```go
client := gai.NewClient(
    gai.WithVoiceAlias("support", gai.VoiceConfig{
        STT:   "deepgram/nova-2",
        LLM:   "anthropic/claude-4-5-sonnet",
        TTS:   "elevenlabs/rachel",
    }),
    gai.WithVoiceAlias("sales", gai.VoiceConfig{
        STT:   "deepgram/nova-2",
        LLM:   "openai/gpt-5",
        TTS:   "cartesia/nova",
    }),
)

// Voice request expands alias
stream, _ := client.Stream(ctx,
    gai.Request("support").  // expands to full voice config
        Audio(userSpeech))
```

### 12.3 Dynamic Routing

```go
client := gai.NewClient(
    gai.WithRouter(func(req gai.Request) string {
        // Route based on request characteristics
        if req.HasImages() {
            return "openai/gpt-5"
        }
        if req.EstimatedTokens() > 10000 {
            return "anthropic/claude-4-5-sonnet"  // larger context
        }
        if req.Metadata["fast"] == true {
            return "groq/llama3-70b-8192"
        }
        return "openai/gpt-5-mini"  // default cheap option
    }),
)

// Requests get routed automatically
result, _ := client.Generate(ctx,
    gai.Request("auto").  // triggers router
        User("Hello").
        Metadata("fast", true))
```

### 12.4 Fallback Chains

```go
client := gai.NewClient(
    gai.WithFallback("openai/gpt-5", "anthropic/claude-4-5-sonnet", "gemini/gemini-pro"),
)

// If primary fails (rate limit, error), tries next
result, err := client.Generate(ctx, req)
// result.Provider shows which one succeeded
```

---

## 13. Configuration

### 13.1 Configuration File

```yaml
# gai.yaml
providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
  deepgram:
    api_key: ${DEEPGRAM_API_KEY}
  elevenlabs:
    api_key: ${ELEVENLABS_API_KEY}

defaults:
  model: anthropic/claude-4-5-sonnet
  voice: elevenlabs/rachel
  stt: deepgram/nova-2
  temperature: 0.7

aliases:
  fast: groq/llama3-70b-8192
  smart: anthropic/claude-4-5-sonnet
  cheap: openai/gpt-5-mini

voice_aliases:
  support:
    stt: deepgram/nova-2
    llm: anthropic/claude-4-5-sonnet
    tts: elevenlabs/rachel
```

### 13.2 Loading Configuration

```go
// From file
client, err := gai.NewClientFromConfig("gai.yaml")

// From environment variable
// GAI_CONFIG=/path/to/gai.yaml
client := gai.NewClient()  // auto-loads if GAI_CONFIG set

// Programmatic override
client, err := gai.NewClientFromConfig("gai.yaml",
    gai.WithAlias("custom", "openai/gpt-5"),  // overrides/extends
)
```

### 13.3 Runtime Configuration

```go
// Add provider at runtime
client.RegisterProvider("custom", customProvider)

// Add alias at runtime
client.SetAlias("mymodel", "openai/gpt-5")

// Change defaults
client.SetDefault("model", "anthropic/claude-4-5-sonnet")
```

---

## 14. Provider Implementation

### 14.1 Provider Interface

```go
type Provider interface {
    // Core generation
    Generate(ctx context.Context, req ProviderRequest) (*ProviderResult, error)
    Stream(ctx context.Context, req ProviderRequest) (*ProviderStream, error)

    // Capabilities
    Capabilities() ProviderCapabilities
}

type ProviderCapabilities struct {
    Provider        string
    Models          []string
    SupportsTools   bool
    SupportsVision  bool
    SupportsAudio   bool
    SupportsVideo   bool
    SupportsStreaming bool
    SupportsRealtime bool
    MaxContextTokens int
}
```

### 14.2 STT Provider Interface

```go
type STTProvider interface {
    Transcribe(ctx context.Context, audio []byte, opts STTOptions) (*Transcript, error)
    StreamTranscribe(ctx context.Context, audio io.Reader, opts STTOptions) (*TranscriptStream, error)
    Capabilities() STTCapabilities
}

type STTCapabilities struct {
    Provider   string
    Models     []string
    Languages  []string
    Realtime   bool
    Diarization bool
}
```

### 14.3 TTS Provider Interface

```go
type TTSProvider interface {
    Synthesize(ctx context.Context, text string, opts TTSOptions) (*Audio, error)
    StreamSynthesize(ctx context.Context, text <-chan string, opts TTSOptions) (*AudioStream, error)
    Voices() []Voice
    Capabilities() TTSCapabilities
}

type TTSCapabilities struct {
    Provider  string
    Voices    []Voice
    Languages []string
    Realtime  bool
}

type Voice struct {
    ID          string
    Name        string
    Language    string
    Gender      string
    Description string
    Preview     string  // URL to sample audio
}
```

### 14.4 Low-Level Access

```go
// Access underlying provider for advanced use cases
import "github.com/shillcollin/gai/providers/anthropic"

provider := anthropic.New(
    anthropic.WithAPIKey(key),
    anthropic.WithBetaHeader("max-tokens-4-5-sonnet-2024-07-15"),
)

// Register custom provider
client := gai.NewClient(
    gai.WithProvider("anthropic-beta", provider),
)
```

---

## 15. Package Structure

```
gai/
├── client.go              # Client type and construction
├── request.go             # RequestBuilder
├── result.go              # Result type
├── conversation.go        # Conversation type
├── stream.go              # Stream type and events
├── voice.go               # Voice pipeline orchestration
├── realtime.go            # Realtime session handling
├── aliases.go             # Alias resolution
├── router.go              # Request routing
├── config.go              # Configuration loading
├── errors.go              # Error types
├── options.go             # Functional options
│
├── providers/
│   ├── provider.go        # Provider interface
│   ├── openai/
│   │   ├── client.go      # OpenAI Chat implementation
│   │   ├── realtime.go    # OpenAI Realtime API
│   │   └── options.go
│   ├── openai-responses/
│   │   └── client.go      # OpenAI Responses API
│   ├── anthropic/
│   │   └── client.go
│   ├── gemini/
│   │   ├── client.go
│   │   └── realtime.go    # Gemini Live
│   ├── groq/
│   │   └── client.go
│   ├── xai/
│   │   └── client.go
│   └── compat/
│       └── client.go      # Generic OpenAI-compatible
│
├── stt/
│   ├── provider.go        # STT interface
│   ├── deepgram/
│   │   └── client.go
│   ├── assemblyai/
│   │   └── client.go
│   └── whisper/
│       └── client.go
│
├── tts/
│   ├── provider.go        # TTS interface
│   ├── elevenlabs/
│   │   └── client.go
│   ├── cartesia/
│   │   └── client.go
│   └── playht/
│       └── client.go
│
├── tools/
│   ├── tool.go            # Tool[I,O] generic type
│   └── adapter.go         # ToolHandle adapter
│
├── internal/
│   ├── jsonschema/
│   ├── httpclient/
│   └── stream/
│
└── examples/
    ├── basic/
    ├── streaming/
    ├── structured/
    ├── tools/
    ├── voice/
    ├── realtime/
    └── conversation/
```

---

## 16. Migration Guide

### 16.1 From v1.0 to v2.0

#### Basic Generation

```go
// BEFORE
import "github.com/shillcollin/gai/providers/openai"
import "github.com/shillcollin/gai/core"

client := openai.New(openai.WithAPIKey(key), openai.WithModel("gpt-5"))
result, err := client.GenerateText(ctx, core.Request{
    Messages: []core.Message{
        core.UserMessage(core.TextPart("Hello")),
    },
})
fmt.Println(result.Text)

// AFTER
import "github.com/shillcollin/gai"

client := gai.NewClient()
text, err := client.Text(ctx, gai.Request("openai/gpt-5").User("Hello"))
fmt.Println(text)
```

#### Streaming

```go
// BEFORE
stream, _ := client.StreamText(ctx, core.Request{...})
for event := range stream.Events() {
    if event.Type == core.EventTextDelta {
        fmt.Print(event.TextDelta)
    }
}

// AFTER
stream, _ := client.Stream(ctx, gai.Request("openai/gpt-5").User("Hello"))
for event := range stream.Events() {
    if event.Type == gai.EventTextDelta {
        fmt.Print(event.TextDelta)
    }
}
```

#### Tools

```go
// BEFORE
import "github.com/shillcollin/gai/runner"

r := runner.New(client, runner.WithToolTimeout(30*time.Second))
result, err := r.ExecuteRequest(ctx, core.Request{
    Messages: messages,
    Tools:    tools,
    StopWhen: core.NoMoreTools(),
})

// AFTER
result, err := client.Run(ctx,
    gai.Request("openai/gpt-5").
        Messages(messages...).
        Tools(tools...).
        StopWhen(gai.NoMoreTools()))
```

#### Structured Output

```go
// BEFORE
result, err := core.GenerateObject[Recipe](ctx, client, core.Request{...})

// AFTER
var recipe Recipe
err := client.Unmarshal(ctx, gai.Request("openai/gpt-5").User("Recipe for pasta"), &recipe)
```

### 16.2 Compatibility Layer

For gradual migration, v2.0 provides compatibility:

```go
import "github.com/shillcollin/gai/compat/v1"

// Wrap v2 client in v1 interface
legacyClient := v1.WrapProvider(client, "openai/gpt-5")

// Use with existing v1 code
result, err := legacyClient.GenerateText(ctx, core.Request{...})
```

---

## 17. API Reference

### 17.1 Client Methods

```go
// Construction
func NewClient(opts ...ClientOption) *Client
func NewClientFromConfig(path string, opts ...ClientOption) (*Client, error)

// Generation
func (c *Client) Generate(ctx context.Context, req *RequestBuilder) (*Result, error)
func (c *Client) Text(ctx context.Context, req *RequestBuilder) (string, error)
func (c *Client) Image(ctx context.Context, req *RequestBuilder) (*Image, error)
func (c *Client) Unmarshal(ctx context.Context, req *RequestBuilder, v any) error

// Agentic
func (c *Client) Run(ctx context.Context, req *RequestBuilder) (*Result, error)

// Streaming
func (c *Client) Stream(ctx context.Context, req *RequestBuilder) (*Stream, error)
func (c *Client) StreamRun(ctx context.Context, req *RequestBuilder) (*Stream, error)

// Voice (low-level)
func (c *Client) Transcribe(ctx context.Context, audio []byte, opts ...TranscribeOption) (string, error)
func (c *Client) Synthesize(ctx context.Context, text string, opts ...SynthesizeOption) ([]byte, error)
func (c *Client) StreamSynthesize(ctx context.Context, text <-chan string, opts ...SynthesizeOption) (*AudioStream, error)

// Realtime
func (c *Client) RealtimeSession(ctx context.Context, cfg RealtimeConfig) (*RealtimeSession, error)

// Conversation
func (c *Client) Conversation(opts ...ConversationOption) *Conversation

// Configuration
func (c *Client) RegisterProvider(id string, provider Provider)
func (c *Client) SetAlias(alias, model string)
func (c *Client) SetDefault(key string, value any)
```

### 17.2 Request Builder Methods

```go
func Request(model string) *RequestBuilder
func (r *RequestBuilder) System(content string) *RequestBuilder
func (r *RequestBuilder) User(content string) *RequestBuilder
func (r *RequestBuilder) Assistant(content string) *RequestBuilder
func (r *RequestBuilder) Messages(msgs ...Message) *RequestBuilder
func (r *RequestBuilder) Image(data []byte, mimeType string) *RequestBuilder
func (r *RequestBuilder) ImageURL(url string) *RequestBuilder
func (r *RequestBuilder) Audio(data []byte) *RequestBuilder
func (r *RequestBuilder) File(data []byte, mimeType string) *RequestBuilder
func (r *RequestBuilder) Tools(tools ...ToolHandle) *RequestBuilder
func (r *RequestBuilder) ToolChoice(choice ToolChoice) *RequestBuilder
func (r *RequestBuilder) Schema(v any) *RequestBuilder
func (r *RequestBuilder) Voice(voice string) *RequestBuilder
func (r *RequestBuilder) OutputTypes(types ...OutputType) *RequestBuilder
func (r *RequestBuilder) Temperature(t float64) *RequestBuilder
func (r *RequestBuilder) MaxTokens(n int) *RequestBuilder
func (r *RequestBuilder) TopP(p float64) *RequestBuilder
func (r *RequestBuilder) TopK(k int) *RequestBuilder
func (r *RequestBuilder) Stop(sequences ...string) *RequestBuilder
func (r *RequestBuilder) StopWhen(cond StopCondition) *RequestBuilder
func (r *RequestBuilder) MaxSteps(n int) *RequestBuilder
func (r *RequestBuilder) OnStop(finalizer Finalizer) *RequestBuilder
func (r *RequestBuilder) Option(key string, value any) *RequestBuilder
func (r *RequestBuilder) Metadata(key string, value any) *RequestBuilder
```

### 17.3 Result Methods

```go
func (r *Result) Text() string
func (r *Result) HasText() bool
func (r *Result) Image() *Image
func (r *Result) Images() []Image
func (r *Result) HasImage() bool
func (r *Result) Audio() *Audio
func (r *Result) AudioData() []byte
func (r *Result) HasAudio() bool
func (r *Result) ToolCalls() []ToolCall
func (r *Result) HasToolCalls() bool
func (r *Result) Messages() []Message
```

### 17.4 Conversation Methods

```go
func (c *Conversation) Say(ctx context.Context, input any, opts ...Option) (*Result, error)
func (c *Conversation) Stream(ctx context.Context, input any, opts ...Option) (*Stream, error)
func (c *Conversation) Run(ctx context.Context, input any, opts ...Option) (*Result, error)
func (c *Conversation) StreamRun(ctx context.Context, input any, opts ...Option) (*Stream, error)
func (c *Conversation) Messages() []Message
func (c *Conversation) Clear()
func (c *Conversation) Rollback(n int)
func (c *Conversation) Fork() *Conversation
func (c *Conversation) MarshalJSON() ([]byte, error)
func (c *Conversation) UnmarshalJSON(data []byte) error
```

---

## 18. Implementation Phases

### Phase 1: Core Client (Foundation) ✅ COMPLETED

**Goal**: Unified client with model strings, basic generation

**Tasks**:
- [x] `Client` type with provider registry
- [x] Model string parsing and resolution
- [x] Environment-based auto-configuration
- [x] `RequestBuilder` fluent API
- [x] `Result` type with multimodal parts
- [x] `Generate()`, `Text()`, `Unmarshal()` methods
- [x] `Stream()` method with events
- [x] Adapt existing providers to new interface (register.go files)
- [x] Basic aliases
- [x] `Run()` and `StreamRun()` methods (integrated runner)

**Deliverable**: Can replace v1 for text generation

**Implementation Notes** (December 2024):
- Created `registry.go` with global provider registry using self-registration pattern
- Created `errors.go` with `ModelError`, `ProviderError`, and sentinel errors
- Created `request.go` with fluent `RequestBuilder` API
- Created `result.go` with `Result` wrapper and convenience accessors
- Created `stream.go` with `Stream` wrapper
- Created `client.go` with `Client` type and all execution methods
- Created `options.go` with `ClientOption` functional options
- Created `aliases.go` with runtime alias management
- Created `register.go` for each provider (openai, anthropic, gemini, groq, xai)
- Created comprehensive test suite (60+ tests)
- Created example files in `examples/v2/`

### Phase 2: Agentic Loop (Tools) ✅ COMPLETED

**Goal**: Integrated tool execution

**Tasks**:
- [x] `Run()` method (replaces Runner)
- [x] `StreamRun()` method
- [x] Stop conditions (NoMoreTools, MaxSteps, MaxTokens, MaxCost, Custom, Any, All)
- [x] Step tracking in Result
- [x] Tool timeout/retry from Runner
- [x] Memoization support
- [x] OnStop finalizer callback
- [x] Runner type re-exports at gai package level

**Deliverable**: Full tool support without separate Runner

**Implementation Notes** (December 2024):
- `Run()` and `StreamRun()` delegate to existing `runner` package
- Added `MaxTokens(n)`, `MaxCost(usd)`, and `Custom(fn)` stop conditions to `core/stop_conditions.go`
- Added corresponding `StopReasonMaxTokens`, `StopReasonMaxCost`, `StopReasonCustom` constants
- Re-exported all stop conditions and combinators in `stop_conditions.go`
- Created `runner_types.go` with type re-exports: `ToolRetry`, `ToolMemo`, `Interceptor`, `ToolErrorMode`, `FinalState`, `Finalizer`, `Step`, `ToolExecution`
- `OnStop(finalizer)` allows post-execution callbacks (e.g., summarize when hitting MaxSteps)
- Enhanced `internal/testutil/mock_provider.go` with `MultiStepMockProvider` for agentic testing
- Comprehensive test coverage in `client_test.go` and `stop_conditions_test.go`

### Phase 3: Voice Pipeline (STT/TTS) ✅ COMPLETED

**Goal**: Voice as input/output modality

**Tasks**:
- [x] STT provider interface
- [x] TTS provider interface
- [x] Deepgram STT provider
- [x] ElevenLabs, Cartesia TTS providers
- [x] `Transcribe()`, `Synthesize()` methods
- [x] `Audio()` request option (auto-STT via `STT()` method)
- [x] `Voice()` request option (auto-TTS)
- [x] Streaming voice (audio deltas via `StreamSynthesize()`)
- [x] Voice aliases (`WithVoiceAlias()`)

**Deliverable**: Full voice support

**Implementation Notes** (December 2024):
- Created `stt/provider.go` with `Provider` interface, `Transcript`, `TranscriptStream`, `TranscriptEvent` types
- Created `stt_registry.go` with global STT provider registry using self-registration pattern
- Created `tts/provider.go` with `Provider` interface, `Audio`, `AudioStream`, `AudioEvent`, `Voice` types
- Created `tts_registry.go` with global TTS provider registry using self-registration pattern
- Implemented Deepgram STT provider (`stt/deepgram/`) with streaming support
- Implemented ElevenLabs TTS provider (`tts/elevenlabs/`) with well-known voices map
- Implemented Cartesia TTS provider (`tts/cartesia/`) with SSE streaming support
- Extended `Client` with `sttProviders`, `ttsProviders`, `voiceAliases` fields
- Added `Transcribe()`, `TranscribeFull()`, `Synthesize()`, `SynthesizeFull()`, `StreamSynthesize()` methods to `Client`
- Extended `RequestBuilder` with `Voice()` and `STT()` methods for specifying TTS/STT providers
- Extended `Result` with `Transcript()`, `AudioData()`, `AudioMIME()`, `HasTranscript()`, `HasAudio()` accessors
- Created `voice.go` with `GenerateVoice()`, `StreamVoice()`, `StreamVoiceRealtime()` for full voice pipelines
- Created `voice_options.go` with `VoiceConfig`, `TranscribeOption`, `SynthesizeOption` types
- Created mock STT/TTS providers in `internal/testutil/` for testing
- Comprehensive test coverage in `voice_test.go`

**Environment Variables**:
| Variable | Provider |
|----------|----------|
| `DEEPGRAM_API_KEY` | Deepgram STT |
| `ELEVENLABS_API_KEY` | ElevenLabs TTS |
| `CARTESIA_API_KEY` | Cartesia TTS |

**Usage Examples**:
```go
// Basic transcription
text, err := client.Transcribe(ctx, audioBytes)
text, err := client.Transcribe(ctx, audioBytes, gai.WithSTT("deepgram/nova-2"))

// Basic synthesis
audio, err := client.Synthesize(ctx, "Hello world")
audio, err := client.Synthesize(ctx, text, gai.WithVoice("elevenlabs/rachel"))

// Full voice pipeline (STT → LLM → TTS)
result, err := client.GenerateVoice(ctx,
    gai.Request("anthropic/claude-4-5-sonnet").
        Audio(userSpeechBytes, "audio/wav").
        STT("deepgram/nova-2").
        Voice("elevenlabs/rachel"))

fmt.Println("User said:", result.Transcript())
fmt.Println("Assistant:", result.Text())
playAudio(result.AudioData())
```

### Phase 4: Conversation & Realtime ✅ COMPLETED

**Goal**: High-level conversation management, realtime APIs

**Tasks**:
- [x] `Conversation` type
- [x] Auto-history management
- [x] Serialization/deserialization
- [x] `RealtimeSession` stub for OpenAI/Gemini (voice pipeline-based)
- [x] Smart routing (realtime vs pipeline) - via `Generate()` detecting audio/voice
- [x] Interruption handling

**Deliverable**: Production-ready voice agents

**Implementation Notes** (December 2024):
- Created `conversation.go` with `Conversation` type and full auto-history management
- Created `conversation_options.go` with `ConversationOption` and `CallOption` functional options
- Added `Client.Conversation()` factory method to `client.go`
- `Conversation.Say()`, `Stream()`, `Run()`, `StreamRun()` methods with auto-history
- History management: `Messages()`, `Clear()`, `Rollback(n)`, `Fork()`, `AddMessages()`
- Full JSON serialization with `MarshalJSON()`/`UnmarshalJSON()` supporting all part types
- Input types: `string`, `AudioInput`, `[]byte` (audio), `core.Message`
- `ConversationStream` wrapper tracks collected text for history updates after streaming
- `maxMsgs` limit with smart trimming (preserves system message)
- Created `realtime.go` with `RealtimeSession` type for continuous voice interaction
- `RealtimeSession` uses voice pipeline (STT → LLM → TTS) under the hood
- `SendAudio()`, `SendText()`, `Events()`, `Interrupt()`, `Close()` methods
- `InputMode` enum: `InputModeAudio`, `InputModeText`, `InputModeBoth`
- VAD placeholder for future voice activity detection
- Created comprehensive test suites in `conversation_test.go` and `realtime_test.go`
- `AudioInput` helper type with `ConvAudio()`, `ConvAudioWithMIME()` constructors

**Conversation Options**:
| Option | Purpose |
|--------|---------|
| `ConvModel(model)` | Set LLM model |
| `ConvSystem(prompt)` | Set system prompt |
| `ConvVoice(voice)` | Set TTS voice |
| `ConvSTT(provider)` | Set STT provider |
| `ConvTools(tools...)` | Add tools |
| `ConvMessages(msgs...)` | Seed with existing messages |
| `ConvMaxMessages(n)` | Limit history size |
| `ConvMetadata(k, v)` | Set metadata |

**Call Options** (per-call overrides):
| Option | Purpose |
|--------|---------|
| `WithCallVoice(voice)` | Override voice for one call |
| `WithCallTools(tools...)` | Override tools for one call |

**Usage Examples**:
```go
// Basic conversation
conv := client.Conversation(
    gai.ConvModel("anthropic/claude-4-5-sonnet"),
    gai.ConvSystem("You are a helpful assistant"),
)

reply, _ := conv.Say(ctx, "Hello!")
fmt.Println(reply.Text())

reply, _ = conv.Say(ctx, "What did I just say?")
fmt.Println(reply.Text())  // Remembers context

// Streaming with auto-history
stream, _ := conv.Stream(ctx, "Tell me a story")
for event := range stream.Events() {
    if event.Type == core.EventTextDelta {
        fmt.Print(event.TextDelta)
    }
}
stream.Close()  // Updates history

// Fork conversation
branch := conv.Fork()
branch.Say(ctx, "Different path...")  // Independent history

// Serialize/restore
data, _ := json.Marshal(conv)
conv2 := client.Conversation()
json.Unmarshal(data, conv2)

// Realtime session (voice)
session, _ := client.RealtimeSession(ctx, gai.RealtimeConfig{
    Model: "anthropic/claude-4-5-sonnet",
    Voice: "elevenlabs/rachel",
    STT:   "deepgram/nova-2",
})
defer session.Close()

session.SendText("Hello!")
for event := range session.Events() {
    // Handle events
}
```

### Phase 5: Polish & Ecosystem

**Goal**: Production hardening, ecosystem

**Tasks**:
- [ ] Configuration file support
- [ ] Dynamic routing
- [ ] Fallback chains
- [ ] Comprehensive examples
- [ ] Migration guide
- [ ] Performance optimization
- [ ] Documentation site

**Deliverable**: v2.0 release

---

## Appendix A: Full Example

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/shillcollin/gai"
)

func main() {
    ctx := context.Background()

    // Create client (auto-configures from environment)
    client := gai.NewClient(
        gai.WithAlias("smart", "anthropic/claude-4-5-sonnet"),
        gai.WithDefaultVoice("elevenlabs/rachel"),
    )

    // Simple text generation
    text, _ := client.Text(ctx,
        gai.Request("openai/gpt-5").
            User("What is the capital of France?"))
    fmt.Println("Answer:", text)

    // Structured output
    type City struct {
        Name       string `json:"name"`
        Country    string `json:"country"`
        Population int    `json:"population"`
    }
    var city City
    client.Unmarshal(ctx,
        gai.Request("smart").
            User("Give me info about Paris"),
        &city)
    fmt.Printf("City: %+v\n", city)

    // Tool usage with agentic loop
    calcTool := gai.NewTool("calculator", "Perform calculations",
        func(ctx context.Context, in struct{ Expression string }, meta gai.ToolMeta) (float64, error) {
            // ... calculate
            return 42, nil
        })

    result, _ := client.Run(ctx,
        gai.Request("smart").
            User("What is 15% of 847?").
            Tools(calcTool).
            StopWhen(gai.NoMoreTools()))
    fmt.Println("Result:", result.Text())

    // Conversation with voice
    conv := client.Conversation(
        gai.Model("smart"),
        gai.System("You are a helpful assistant"),
    )

    // Text turn
    reply, _ := conv.Say(ctx, "Hello!")
    fmt.Println("Assistant:", reply.Text())

    // Voice turn (if we had audio input)
    audioInput, _ := os.ReadFile("user_speech.wav")
    reply, _ = conv.Say(ctx,
        gai.Audio(audioInput),
        gai.Voice("elevenlabs/rachel"))

    fmt.Println("User said:", reply.Transcript)
    fmt.Println("Assistant:", reply.Text())

    // Play audio response
    os.WriteFile("response.mp3", reply.AudioData(), 0644)

    // Stream with voice
    stream, _ := conv.Stream(ctx,
        gai.Audio(audioInput),
        gai.Voice("elevenlabs/rachel"))

    for event := range stream.Events() {
        switch event.Type {
        case gai.EventTranscript:
            fmt.Println("User:", event.Transcript)
        case gai.EventTextDelta:
            fmt.Print(event.TextDelta)
        case gai.EventAudioDelta:
            // Stream to speaker
        }
    }
}
```

---

*Document Version: 1.0*
*Last Updated: December 2024*
*Status: Design Specification*

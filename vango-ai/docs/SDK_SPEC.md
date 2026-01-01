# Vango AI Go SDK Specification

**Version:** 1.0.0  
**Date:** December 2025  
**Status:** Implementation Ready

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Design Philosophy](#2-design-philosophy)
3. [Package Structure](#3-package-structure)
4. [Client](#4-client)
5. [Messages Service](#5-messages-service)
6. [Request Building](#6-request-building)
7. [Response Handling](#7-response-handling)
8. [Streaming](#8-streaming)
9. [Tool Loop (Run)](#9-tool-loop-run)
10. [Tools](#10-tools)
11. [Conversation Management](#11-conversation-management)
12. [Voice & Audio](#12-voice--audio)
13. [Live Sessions](#13-live-sessions)
14. [Error Handling](#14-error-handling)
15. [Observability](#15-observability)
16. [Testing](#16-testing)
17. [Examples](#17-examples)

---

## 1. Executive Summary

The Vango AI Go SDK is a **hybrid library** operating in two modes:

### Direct Mode (Default)
Runs the translation engine (`pkg/core`) directly in your process. No external proxy required. Provider API keys come from environment variables.

```go
// Direct Mode: SDK imports pkg/core, calls providers directly
// ANTHROPIC_API_KEY, OPENAI_API_KEY read from env
client := vango.NewClient()
```

### Proxy Mode
Connects to a Vango AI Proxy instance via HTTP. Ideal for production environments requiring centralized governance, observability, and secret management.

```go
// Proxy Mode: SDK is a thin HTTP client to the proxy
client := vango.NewClient(
    vango.WithBaseURL("http://vango-proxy.internal:8080"),
    vango.WithAPIKey("vango_sk_..."),
)
```

### Both Modes Support:
- **All LLM providers** through a unified interface
- **Streaming and non-streaming** requests
- **Tool execution loops** with configurable stop conditions
- **Voice pipelines** (STT → LLM → TTS)
- **Real-time sessions** for bidirectional voice
- **Type-safe tools** via generics

### Quick Start

```go
import "github.com/vango-ai/vango"

// Direct Mode (no proxy needed)
client := vango.NewClient()

resp, err := client.Messages.Create(ctx, &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("Hello!")},
    },
})

fmt.Println(resp.TextContent())
```

---

## 2. Design Philosophy

### 2.1 Shared Core Architecture

The SDK and Proxy share the same logic (`pkg/core`). This guarantees:
- **Zero drift**: Direct Mode and Proxy Mode behave identically
- **Single source of truth**: One implementation for translation, tools, voice
- **Flexible deployment**: Start with Direct Mode, migrate to Proxy when needed

### 2.2 Client-Side Orchestration

The SDK handles tool execution loops, not the server. This enables:
- Model swapping mid-conversation
- Custom stop conditions
- Full visibility into each step
- Tool handlers run in YOUR process

### 2.3 Explicit Over Magic

No hidden state, no implicit behaviors. Every model call, tool execution, and decision point is visible and controllable.

### 2.4 Composable Primitives

Simple building blocks compose into complex workflows:
```go
// Wrap Run() as a tool for hierarchical agents
researchTool := vango.FuncAsTool("research", "...", func(ctx, input) (string, error) {
    result, _ := client.Messages.Run(ctx, innerRequest, vango.WithMaxToolCalls(10))
    return result.Response.TextContent(), nil
})
```

### 2.5 Progressive Disclosure

| Level | API | Use Case |
|-------|-----|----------|
| Simple | `Messages.Create()` | Single request/response |
| Streaming | `Messages.Stream()` | Real-time output |
| Agentic | `Messages.Run()` | Tool loops |
| Voice | `Messages.Live()` | Real-time voice |

---

## 3. Package Structure

The SDK lives within the Vango AI monorepo, sharing `pkg/core` with the Proxy.

```
vango/
├── pkg/
│   └── core/                    # Shared Logic (Source of Truth)
│       ├── types/               # Vango AI API types (Request, Response, ContentBlock)
│       ├── providers/           # Provider implementations
│       │   ├── anthropic/       # Anthropic (passthrough)
│       │   ├── openai/          # OpenAI ↔ Vango AI translation
│       │   ├── gemini/          # Gemini ↔ Vango AI translation
│       │   └── groq/            # Groq (OpenAI-compat)
│       ├── voice/               # Voice pipeline
│       │   ├── pipeline.go      # STT → LLM → TTS orchestration
│       │   ├── stt/             # Deepgram, Whisper
│       │   └── tts/             # ElevenLabs, OpenAI TTS
│       └── tools/               # Tool normalization
│           └── normalize.go
│
├── sdk/                         # The Go SDK
│   ├── client.go                # Client, NewClient()
│   ├── messages.go              # MessagesService
│   ├── audio.go                 # AudioService
│   ├── models.go                # ModelsService
│   ├── request.go               # MessageRequest, builders
│   ├── response.go              # Response types
│   ├── stream.go                # Stream, event handling
│   ├── run.go                   # Run, RunStream, stop conditions
│   ├── live.go                  # LiveSession
│   ├── tools.go                 # Tool, FuncAsTool
│   ├── content.go               # Content block constructors
│   ├── conversation.go          # Conversation helper
│   ├── errors.go                # Error types
│   ├── options.go               # Functional options
│   └── internal/
│       ├── direct/              # Direct mode (uses pkg/core)
│       └── proxy/               # Proxy mode (HTTP client)
│
├── cmd/
│   └── vango-proxy/             # The HTTP server binary
│       └── main.go
│
├── api/
│   └── openapi.yaml             # OpenAPI spec for non-Go SDKs
│
└── docker/
    └── Dockerfile
```

---

## 4. Client

### 4.1 Client Modes

The client auto-detects mode based on configuration:

```go
// DIRECT MODE (Default)
// No BaseURL provided. SDK imports pkg/core and calls providers directly.
// Requires provider keys in environment (ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.)
client := vango.NewClient()

// PROXY MODE
// BaseURL provided. SDK becomes a thin HTTP client to the Vango AI Proxy.
// Keys are managed by the Proxy; you only need a Vango AI API key.
client := vango.NewClient(
    vango.WithBaseURL("http://localhost:8080"),
    vango.WithAPIKey("vango_sk_..."),
)
```

### 4.2 Client Structure

```go
type Client struct {
    Messages *MessagesService
    Audio    *AudioService
    Models   *ModelsService
    
    // Internal
    mode       clientMode  // direct or proxy
    baseURL    string      // Only used in proxy mode
    apiKey     string      // Vango AI API key (proxy mode) or unused (direct mode)
    httpClient *http.Client
    logger     *slog.Logger
    tracer     trace.Tracer
    
    // Only used in direct mode
    core       *core.Engine
}
```

### 4.3 Constructor

```go
// Direct Mode (default)
client := vango.NewClient()

// Proxy Mode
client := vango.NewClient(
    vango.WithBaseURL("http://localhost:8080"),
    vango.WithAPIKey("vango_sk_..."),
)

// Direct Mode with explicit provider keys
client := vango.NewClient(
    vango.WithProviderKey("anthropic", "sk-ant-..."),
    vango.WithProviderKey("openai", "sk-..."),
)

// With additional options
client := vango.NewClient(
    vango.WithHTTPClient(customClient),
    vango.WithTimeout(30 * time.Second),
    vango.WithLogger(slog.Default()),
    vango.WithTracer(otel.Tracer("my-app")),
    vango.WithRetries(3),
)
```

### 4.4 Client Options

```go
type ClientOption func(*Client)

// Mode selection (implicit)
func WithBaseURL(url string) ClientOption     // Enables Proxy Mode
func WithAPIKey(key string) ClientOption      // Vango AI API key for Proxy Mode

// Direct Mode provider keys
func WithProviderKey(provider, key string) ClientOption

// HTTP configuration
func WithHTTPClient(c *http.Client) ClientOption
func WithTimeout(d time.Duration) ClientOption

// Observability
func WithLogger(l *slog.Logger) ClientOption
func WithTracer(t trace.Tracer) ClientOption

// Resilience
func WithRetries(n int) ClientOption
func WithRetryBackoff(d time.Duration) ClientOption
```

---

## 5. Messages Service

### 5.1 Service Structure

```go
type MessagesService struct {
    client *Client
}
```

### 5.2 Methods

```go
// Single request/response
func (s *MessagesService) Create(ctx context.Context, req *MessageRequest) (*Response, error)

// Streaming response
func (s *MessagesService) Stream(ctx context.Context, req *MessageRequest) (*Stream, error)

// Tool execution loop (blocking)
func (s *MessagesService) Run(ctx context.Context, req *MessageRequest, opts ...RunOption) (*RunResult, error)

// Tool execution loop (streaming)
func (s *MessagesService) RunStream(ctx context.Context, req *MessageRequest, opts ...RunOption) (*RunStream, error)

// Real-time bidirectional session
func (s *MessagesService) Live(ctx context.Context, cfg *LiveConfig) (*LiveSession, error)

// Structured Data Extraction
// Automatically generates schema from dest, executes request, and unmarshals result.
func (s *MessagesService) Extract(ctx context.Context, req *MessageRequest, dest any) (*Response, error)
```

---

## 6. Request Building

### 6.1 MessageRequest

```go
type MessageRequest struct {
    Model    string    `json:"model"`
    Messages []Message `json:"messages"`
    
    // Optional
    MaxTokens      int              `json:"max_tokens,omitempty"`
    System         any              `json:"system,omitempty"` // string or []ContentBlock
    Temperature    *float64         `json:"temperature,omitempty"`
    TopP           *float64         `json:"top_p,omitempty"`
    TopK           *int             `json:"top_k,omitempty"`
    StopSequences  []string         `json:"stop_sequences,omitempty"`
    Tools          []Tool           `json:"tools,omitempty"`
    ToolChoice     *ToolChoice      `json:"tool_choice,omitempty"`
    Stream         bool             `json:"stream,omitempty"`
    OutputFormat   *OutputFormat    `json:"output_format,omitempty"`
    Voice          *VoiceConfig     `json:"voice,omitempty"`
    Output         *OutputConfig    `json:"output,omitempty"`
    Extensions     map[string]any   `json:"extensions,omitempty"`
    Metadata       map[string]any   `json:"metadata,omitempty"`
}

type OutputFormat struct {
    Type       string      `json:"type"` // "json_schema"
    JSONSchema *JSONSchema `json:"json_schema"`
}
```

### 6.2 Message

```go
type Message struct {
    Role    string `json:"role"` // "user", "assistant"
    Content any    `json:"content"` // string or []ContentBlock
}
```

### 6.3 Content Block Constructors

```go
// Text
vango.Text("Hello, world!")

// Image from bytes
vango.Image(data, "image/png")

// Image from URL
vango.ImageURL("https://example.com/image.png")

// Audio
vango.Audio(data, "audio/wav")

// Video
vango.Video(data, "video/mp4")

// Document
vango.Document(data, "application/pdf", "report.pdf")

// Tool result
vango.ToolResult(toolUseID, []ContentBlock{vango.Text("result")})
```

### 6.4 Usage Examples

```go
// Simple text
req := &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("Hello!")},
    },
}

// With image
req := &vango.MessageRequest{
    Model: "openai/gpt-4o",
    Messages: []vango.Message{
        {Role: "user", Content: []vango.ContentBlock{
            vango.Text("What's in this image?"),
            vango.ImageURL("https://example.com/image.png"),
        }},
    },
}

// With tools
req := &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("What's the weather in Tokyo?")},
    },
    Tools: []vango.Tool{
        vango.WebSearch(),
        weatherTool,
    },
}

// With voice
req := &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: []vango.Message{
        {Role: "user", Content: []vango.ContentBlock{
            vango.Audio(audioData, "audio/wav"),
        }},
    },
    Voice: &vango.VoiceConfig{
        Input:  &vango.VoiceInputConfig{Provider: "deepgram", Model: "nova-2"},
        Output: &vango.VoiceOutputConfig{Provider: "elevenlabs", Voice: "rachel"},
    },
}
```

### 6.5 Structured Output

```go
type UserInfo struct {
    Name    string   `json:"name" desc:"The user's full name"`
    Age     int      `json:"age" desc:"Age in years"`
    Skills  []string `json:"skills" desc:"List of technical skills"`
    Role    string   `json:"role" enum:"admin,user,guest"` // Enum validation
}

// Usage
var info UserInfo
resp, err := client.Messages.Extract(ctx, &vango.MessageRequest{
    Model: "openai/gpt-4o",
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("John Doe is a 30 year old Go developer.")},
    },
}, &info)
```

### 6.6 Content Marshaling Rules

The SDK handles flexible input types for `Message.Content` to ensure developer ergonomics while maintaining API consistency.

| Input Type | Marshaled JSON | Note |
|------------|----------------|------|
| `string` | `"Hello"` | Simple text input |
| `vango.ContentBlock` | `[{"type": "text", "text": "..."}]` | Automatically wrapped in array |
| `[]vango.ContentBlock`| `[{"type": "text", ...}, ...]` | Passed as is |

---

## 7. Response Handling

### 7.1 Response Type

```go
type Response struct {
    ID           string         `json:"id"`
    Type         string         `json:"type"`
    Role         string         `json:"role"`
    Model        string         `json:"model"`
    Content      []ContentBlock `json:"content"`
    StopReason   string         `json:"stop_reason"`
    StopSequence *string        `json:"stop_sequence,omitempty"`
    Usage        Usage          `json:"usage"`
}

type Usage struct {
    InputTokens       int      `json:"input_tokens"`
    OutputTokens      int      `json:"output_tokens"`
    TotalTokens       int      `json:"total_tokens"`
    CacheReadTokens   *int     `json:"cache_read_tokens,omitempty"`
    CacheWriteTokens  *int     `json:"cache_write_tokens,omitempty"`
    CostUSD           *float64 `json:"cost_usd,omitempty"`
}
```

### 7.2 Convenience Methods

```go
// Get all text content concatenated
func (r *Response) TextContent() string

// Get all tool use blocks
func (r *Response) ToolUses() []ToolUseBlock

// Check if response has tool calls
func (r *Response) HasToolUse() bool

// Get thinking blocks (if present)
func (r *Response) ThinkingContent() string

// Get first image block
func (r *Response) ImageContent() *ImageBlock

// Get first audio block
func (r *Response) AudioContent() *AudioBlock
```

### 7.3 Content Block Types

```go
type TextBlock struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"`
}

type ToolUseBlock struct {
    Type  string         `json:"type"` // "tool_use"
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input"`
}

type ThinkingBlock struct {
    Type     string  `json:"type"` // "thinking"
    Thinking string  `json:"thinking"`
    Summary  *string `json:"summary,omitempty"`
}

type ImageBlock struct {
    Type   string      `json:"type"` // "image"
    Source ImageSource `json:"source"`
}

type AudioBlock struct {
    Type       string      `json:"type"` // "audio"
    Source     AudioSource `json:"source"`
    Transcript *string     `json:"transcript,omitempty"`
}
```

---

## 8. Streaming

### 8.1 Stream Type

```go
type Stream struct {
    events   <-chan StreamEvent
    response *Response // Populated after stream ends
    err      error
    closed   atomic.Bool
}

func (s *Stream) Events() <-chan StreamEvent
func (s *Stream) Response() *Response
func (s *Stream) Err() error
func (s *Stream) Close() error
```

### 8.2 Stream Events

```go
type StreamEvent interface {
    eventType() string
}

type MessageStartEvent struct {
    Type    string   `json:"type"`
    Message Response `json:"message"`
}

type ContentBlockStartEvent struct {
    Type         string       `json:"type"`
    Index        int          `json:"index"`
    ContentBlock ContentBlock `json:"content_block"`
}

type ContentBlockDeltaEvent struct {
    Type  string `json:"type"`
    Index int    `json:"index"`
    Delta Delta  `json:"delta"`
}

type TextDelta struct {
    Type string `json:"type"` // "text_delta"
    Text string `json:"text"`
}

type InputJSONDelta struct {
    Type        string `json:"type"` // "input_json_delta"
    PartialJSON string `json:"partial_json"`
}

type ThinkingDelta struct {
    Type     string `json:"type"` // "thinking_delta"
    Thinking string `json:"thinking"`
}

type AudioDeltaEvent struct {
    Type  string `json:"type"`
    Delta struct {
        Data   string `json:"data"` // base64
        Format string `json:"format"`
    } `json:"delta"`
}

type TranscriptDeltaEvent struct {
    Type  string `json:"type"`
    Role  string `json:"role"`
    Delta struct {
        Text string `json:"text"`
    } `json:"delta"`
}

type ContentBlockStopEvent struct {
    Type  string `json:"type"`
    Index int    `json:"index"`
}

type MessageDeltaEvent struct {
    Type  string `json:"type"`
    Delta struct {
        StopReason string `json:"stop_reason,omitempty"`
    } `json:"delta"`
    Usage Usage `json:"usage"`
}

type MessageStopEvent struct {
    Type string `json:"type"`
}

type ErrorEvent struct {
    Type  string `json:"type"`
    Error Error  `json:"error"`
}
```

### 8.3 Streaming Usage

```go
stream, err := client.Messages.Stream(ctx, req)
if err != nil {
    return err
}
defer stream.Close()

for event := range stream.Events() {
    switch e := event.(type) {
    case *vango.ContentBlockDeltaEvent:
        if delta, ok := e.Delta.(*vango.TextDelta); ok {
            fmt.Print(delta.Text)
        }
    case *vango.AudioDeltaEvent:
        audioData, _ := base64.StdEncoding.DecodeString(e.Delta.Data)
        speaker.Write(audioData)
    case *vango.ErrorEvent:
        return e.Error
    }
}

// Get final response after stream ends
resp := stream.Response()
fmt.Printf("\nTotal tokens: %d\n", resp.Usage.TotalTokens)
```

---

## 9. Tool Loop (Run)

### 9.1 RunResult

```go
type RunResult struct {
    Response      *Response  // Final response
    Steps         []RunStep  // All steps taken
    ToolCallCount int        // Total tool calls
    TurnCount     int        // Total LLM turns
    Usage         Usage      // Aggregated usage
    StopReason    string     // Why the loop stopped
}

type RunStep struct {
    Index        int
    Response     *Response
    ToolCalls    []ToolCall
    ToolResults  []ToolResult
    DurationMs   int64
}

type ToolCall struct {
    ID    string
    Name  string
    Input map[string]any
}

type ToolResult struct {
    ToolUseID string
    Content   []ContentBlock
    Error     error
}
```

### 9.2 Run Options

```go
type RunOption func(*runConfig)

// Stop conditions
func WithMaxToolCalls(n int) RunOption
func WithMaxTurns(n int) RunOption
func WithMaxTokens(n int) RunOption
func WithTimeout(d time.Duration) RunOption
func WithStopWhen(fn func(*Response) bool) RunOption

// Tool handlers
func WithToolHandler(name string, fn ToolHandler) RunOption
func WithToolHandlers(handlers map[string]ToolHandler) RunOption

// Hooks
func WithBeforeCall(fn func(*MessageRequest)) RunOption
func WithAfterResponse(fn func(*Response)) RunOption
func WithOnToolCall(fn func(name string, input map[string]any, output any, err error)) RunOption
func WithOnStop(fn func(*RunResult)) RunOption

// Behavior
func WithParallelTools(enabled bool) RunOption
func WithToolTimeout(d time.Duration) RunOption
```

### 9.3 Usage Examples

```go
// Basic Run
result, err := client.Messages.Run(ctx, req,
    vango.WithMaxToolCalls(10),
)
fmt.Println(result.Response.TextContent())

// With custom stop condition
result, err := client.Messages.Run(ctx, req,
    vango.WithStopWhen(func(r *vango.Response) bool {
        return strings.Contains(r.TextContent(), "DONE")
    }),
    vango.WithMaxTurns(5), // Safety limit
)

// With tool handlers
result, err := client.Messages.Run(ctx, req,
    vango.WithToolHandler("get_weather", func(ctx context.Context, input json.RawMessage) (any, error) {
        var params struct {
            Location string `json:"location"`
        }
        json.Unmarshal(input, &params)
        return getWeather(params.Location), nil
    }),
)

// With hooks for logging
result, err := client.Messages.Run(ctx, req,
    vango.WithBeforeCall(func(req *vango.MessageRequest) {
        log.Printf("Calling %s with %d messages", req.Model, len(req.Messages))
    }),
    vango.WithOnToolCall(func(name string, input map[string]any, output any, err error) {
        log.Printf("Tool %s: %v -> %v (err: %v)", name, input, output, err)
    }),
)
```

### 9.4 RunStream

```go
stream, err := client.Messages.RunStream(ctx, req,
    vango.WithMaxToolCalls(10),
)
if err != nil {
    return err
}
defer stream.Close()

for event := range stream.Events() {
    switch e := event.(type) {
    case *vango.ContentBlockDeltaEvent:
        fmt.Print(e.Delta.(*vango.TextDelta).Text)
    case *vango.ToolCallStartEvent:
        fmt.Printf("\n[Calling %s...]\n", e.Name)
    case *vango.ToolResultEvent:
        fmt.Printf("[Result received]\n")
    case *vango.StepCompleteEvent:
        fmt.Printf("\n--- Step %d complete ---\n", e.Index)
    }
}

result := stream.Result()
fmt.Printf("Total tool calls: %d\n", result.ToolCallCount)
```

---

## 10. Tools

### 10.1 Tool Type

```go
type Tool struct {
    Type        string      `json:"type"` // "function", "web_search", etc.
    Name        string      `json:"name,omitempty"`
    Description string      `json:"description,omitempty"`
    InputSchema *JSONSchema `json:"input_schema,omitempty"`
    Config      any         `json:"config,omitempty"`
    
    // Internal: handler for function tools
    handler ToolHandler
}

type ToolHandler func(ctx context.Context, input json.RawMessage) (any, error)
```

### 10.2 Native Tools

```go
// Web search
vango.WebSearch()
vango.WebSearch(vango.WebSearchConfig{MaxResults: 5})

// Code execution
vango.CodeExecution()
vango.CodeExecution(vango.CodeExecConfig{Languages: []string{"python"}})

// Computer use
vango.ComputerUse(1920, 1080)

// Text editor
vango.TextEditor()

// File search (OpenAI)
vango.FileSearch()
```

### 10.3 Manual Tool Definition

```go
tool := vango.Tool{
    Type:        "function",
    Name:        "get_stock_price",
    Description: "Get current stock price for a ticker symbol",
    InputSchema: &vango.JSONSchema{
        Type: "object",
        Properties: map[string]vango.JSONSchema{
            "symbol": {
                Type:        "string",
                Description: "Stock ticker symbol (e.g., AAPL)",
            },
        },
        Required: []string{"symbol"},
    },
}
```

### 10.4 FuncAsTool (Type-Safe)

```go
// Automatically generates schema from struct
weatherTool := vango.FuncAsTool(
    "get_weather",
    "Get current weather for a location",
    func(ctx context.Context, input struct {
        Location string `json:"location" desc:"City name or coordinates"`
        Units    string `json:"units" desc:"celsius or fahrenheit" enum:"celsius,fahrenheit"`
    }) (*WeatherData, error) {
        return weatherAPI.Get(input.Location, input.Units)
    },
)

// Use in request
req := &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: messages,
    Tools: []vango.Tool{weatherTool},
}

// Tool is automatically executed during Run()
result, _ := client.Messages.Run(ctx, req)
```

### 10.5 Nested Run as Tool

```go
// Create a "deep research" tool that runs its own agent loop
deepResearch := vango.FuncAsTool(
    "deep_research",
    "Perform thorough multi-step research on a topic",
    func(ctx context.Context, input struct {
        Topic string `json:"topic"`
        Depth int    `json:"depth"`
    }) (string, error) {
        innerReq := &vango.MessageRequest{
            Model:  "anthropic/claude-sonnet-4",
            System: "Research thoroughly. Search, cross-reference, synthesize.",
            Messages: []vango.Message{
                {Role: "user", Content: vango.Text("Research: " + input.Topic)},
            },
            Tools: []vango.Tool{vango.WebSearch()},
        }
        
        result, err := client.Messages.Run(ctx, innerReq,
            vango.WithMaxToolCalls(input.Depth * 3),
        )
        if err != nil {
            return "", err
        }
        
        return result.Response.TextContent(), nil
    },
)

// Outer agent uses deep_research as a tool
result, _ := client.Messages.Run(ctx, &vango.MessageRequest{
    Model: "groq/llama-3.3-70b", // Fast model orchestrates
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("Compare AI strategies of major tech companies")},
    },
    Tools: []vango.Tool{deepResearch},
})
```

---

## 11. Conversation Management

### 11.1 Manual Conversation

```go
messages := []vango.Message{
    {Role: "user", Content: vango.Text("Hello!")},
}

resp, _ := client.Messages.Create(ctx, &vango.MessageRequest{
    Model:    "anthropic/claude-sonnet-4",
    Messages: messages,
})

// Append assistant response
messages = append(messages, vango.Message{
    Role:    "assistant",
    Content: resp.Content,
})

// Continue conversation
messages = append(messages, vango.Message{
    Role:    "user",
    Content: vango.Text("Tell me more"),
})

resp, _ = client.Messages.Create(ctx, &vango.MessageRequest{
    Model:    "anthropic/claude-sonnet-4",
    Messages: messages,
})
```

### 11.2 Conversation Helper

```go
type Conversation struct {
    client   *Client
    messages []Message
    model    string
    system   string
    tools    []Tool
    voice    *VoiceConfig
}

func (c *Conversation) Say(ctx context.Context, input any) (*Response, error)
func (c *Conversation) Stream(ctx context.Context, input any) (*Stream, error)
func (c *Conversation) Run(ctx context.Context, input any, opts ...RunOption) (*RunResult, error)
func (c *Conversation) Messages() []Message
func (c *Conversation) Clear()
func (c *Conversation) Fork() *Conversation
func (c *Conversation) SetModel(model string)
func (c *Conversation) MarshalJSON() ([]byte, error)
func (c *Conversation) UnmarshalJSON(data []byte) error
```

### 11.3 Conversation Usage

```go
conv := vango.NewConversation(client,
    vango.ConvModel("anthropic/claude-sonnet-4"),
    vango.ConvSystem("You are a helpful assistant"),
    vango.ConvTools(vango.WebSearch()),
)

// Simple turns
resp1, _ := conv.Say(ctx, "What's happening in tech news?")
resp2, _ := conv.Say(ctx, "Tell me more about the first item")

// With tool execution
result, _ := conv.Run(ctx, "Research this topic thoroughly",
    vango.WithMaxToolCalls(5),
)

// Fork for branching conversations
branch := conv.Fork()
branch.Say(ctx, "Different path...")

// Serialize for persistence
data, _ := json.Marshal(conv)
os.WriteFile("conversation.json", data, 0644)

// Restore
conv2 := vango.NewConversation(client)
json.Unmarshal(data, conv2)
```

---

## 12. Voice & Audio

### 12.1 Audio Service

```go
type AudioService struct {
    client *Client
}

func (s *AudioService) Transcribe(ctx context.Context, req *TranscribeRequest) (*Transcript, error)
func (s *AudioService) Synthesize(ctx context.Context, req *SynthesizeRequest) (*SynthesisResult, error)
func (s *AudioService) StreamSynthesize(ctx context.Context, req *SynthesizeRequest) (*AudioStream, error)
```

### 12.2 Transcription

```go
transcript, err := client.Audio.Transcribe(ctx, &vango.TranscribeRequest{
    Audio:    audioData,
    Provider: "deepgram",
    Model:    "nova-2",
    Language: "en",
})

fmt.Println(transcript.Text)
fmt.Println(transcript.Confidence)
for _, word := range transcript.Words {
    fmt.Printf("%s (%.2f-%.2f)\n", word.Word, word.Start, word.End)
}
```

### 12.3 Synthesis

```go
result, err := client.Audio.Synthesize(ctx, &vango.SynthesizeRequest{
    Text:     "Hello, how can I help you today?",
    Provider: "elevenlabs",
    Voice:    "rachel",
    Speed:    1.0,
    Format:   "mp3",
})

os.WriteFile("output.mp3", result.Audio, 0644)
```

### 12.4 Streaming Synthesis

```go
stream, err := client.Audio.StreamSynthesize(ctx, &vango.SynthesizeRequest{
    Text:     longText,
    Provider: "elevenlabs",
    Voice:    "rachel",
})

for chunk := range stream.Chunks() {
    speaker.Write(chunk.Data)
}
```

### 12.5 Voice in Messages

```go
// Voice input + output
resp, err := client.Messages.Create(ctx, &vango.MessageRequest{
    Model: "anthropic/claude-sonnet-4",
    Messages: []vango.Message{
        {Role: "user", Content: []vango.ContentBlock{
            vango.Audio(userAudio, "audio/wav"),
        }},
    },
    Voice: &vango.VoiceConfig{
        Input:  &vango.VoiceInputConfig{Provider: "deepgram", Model: "nova-2"},
        Output: &vango.VoiceOutputConfig{Provider: "elevenlabs", Voice: "rachel"},
    },
})

// Response includes transcript and audio
fmt.Println("User said:", resp.Transcript)
fmt.Println("Assistant:", resp.TextContent())
playAudio(resp.AudioContent().Data())
```

---

## 13. Live Sessions

Live Sessions provide real-time bidirectional voice communication using Vango's Universal Voice Pipeline. The same agent definition (model, tools, system prompt) works identically whether you use `Messages.Create()` with audio content blocks or `Messages.Live()` for real-time voice.

### 13.1 Configuration Types

```go
// LiveConfig defines the full agent configuration for a live session.
type LiveConfig struct {
    Model   string       `json:"model"`
    System  string       `json:"system"`
    Tools   []Tool       `json:"tools"`
    Voice   *VoiceConfig `json:"voice"`
}

// VoiceConfig contains all voice-related settings.
type VoiceConfig struct {
    Input     *VoiceInputConfig  `json:"input"`
    Output    *VoiceOutputConfig `json:"output"`
    VAD       *VADConfig         `json:"vad"`
    Interrupt *InterruptConfig   `json:"interrupt"`
}

// VADConfig controls the hybrid voice activity detection.
type VADConfig struct {
    Model             string  `json:"model"`              // Fast LLM for semantic check (default: "anthropic/claude-haiku-4-5-20251001")
    EnergyThreshold   float64 `json:"energy_threshold"`   // RMS threshold (default: 0.02)
    SilenceDurationMs int     `json:"silence_duration_ms"` // Silence before check (default: 600)
    SemanticCheck     bool    `json:"semantic_check"`     // Enable semantic analysis (default: true)
    MinWordsForCheck  int     `json:"min_words_for_check"` // Minimum words (default: 2)
    MaxSilenceMs      int     `json:"max_silence_ms"`     // Force commit timeout (default: 3000)
}

// InterruptConfig controls barge-in detection.
type InterruptConfig struct {
    Mode           string  `json:"mode"`            // "auto", "manual", "disabled" (default: "auto")
    EnergyThreshold float64 `json:"energy_threshold"` // Higher than VAD (default: 0.05)
    DebounceMs      int     `json:"debounce_ms"`     // Minimum sustained speech (default: 100)
    SemanticCheck   bool    `json:"semantic_check"`  // Distinguish interrupts from backchannels (default: true)
    SemanticModel   string  `json:"semantic_model"`  // Fast LLM (default: "anthropic/claude-haiku-4-5-20251001")
    SavePartial     string  `json:"save_partial"`    // "discard", "save", "marked" (default: "marked")
}
```

### 13.2 LiveStream

```go
type LiveStream struct {
    // Send methods
    SendAudio(pcm []byte) error           // Send raw PCM audio (16-bit, mono)
    SendText(text string) error           // Send text directly (for testing)
    Interrupt(transcript string) error    // Force interrupt (skip semantic check)
    Commit() error                        // Force end-of-turn (push-to-talk)
    UpdateConfig(cfg *LiveConfig) error   // Update model/voice/tools mid-session

    // Receive
    Events() <-chan LiveEvent

    // Lifecycle
    Close() error
}
```

### 13.3 Live Events

```go
// Session lifecycle
type SessionCreatedEvent struct {
    SessionID  string `json:"session_id"`
    SampleRate int    `json:"sample_rate"`
    Channels   int    `json:"channels"`
}

// VAD status events
type VADStatusEvent struct {
    Type       string `json:"type"` // "vad.listening", "vad.analyzing", "vad.silence"
    DurationMs int    `json:"duration_ms,omitempty"`
}

// Transcription events
type TranscriptDeltaEvent struct {
    Delta string `json:"delta"`
}

type InputCommittedEvent struct {
    Transcript string `json:"transcript"`
}

// Audio output
type AudioChunkEvent struct {
    Data []byte // Raw PCM audio
}

// Interrupt events
type InterruptDetectingEvent struct {
    Transcript string `json:"transcript"`
}

type InterruptDismissedEvent struct {
    Transcript string `json:"transcript"`
    Reason     string `json:"reason"` // "backchannel", "noise", etc.
}

type ResponseInterruptedEvent struct {
    PartialText         string `json:"partial_text"`
    InterruptTranscript string `json:"interrupt_transcript"`
    AudioPositionMs     int    `json:"audio_position_ms"`
}

// Standard streaming events (reused from Messages.Stream)
type ContentBlockStartEvent struct { ... }
type ContentBlockDeltaEvent struct { ... }
type ContentBlockStopEvent struct { ... }
type MessageStopEvent struct { ... }

// Error
type ErrorEvent struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

### 13.4 Live Usage Example

```go
session, err := client.Messages.Live(ctx, &vango.LiveConfig{
    Model:  "anthropic/claude-sonnet-4-20250514",
    System: "You are a helpful voice assistant.",
    Tools:  []vango.Tool{vango.WebSearch()},
    Voice: &vango.VoiceConfig{
        Input:  &vango.VoiceInputConfig{Provider: "cartesia", Language: "en"},
        Output: &vango.VoiceOutputConfig{Provider: "cartesia", Voice: "a0e99841-438c-4a64-b679-ae501e7d6091"},
        VAD: &vango.VADConfig{
            Model:             "anthropic/claude-haiku-4-5-20251001",
            SilenceDurationMs: 600,
            SemanticCheck:     true,
        },
        Interrupt: &vango.InterruptConfig{
            Mode:          "auto",
            SemanticCheck: true,
        },
    },
})
if err != nil {
    return err
}
defer session.Close()

// Handle events
go func() {
    for event := range session.Events() {
        switch e := event.(type) {
        case *vango.VADStatusEvent:
            fmt.Printf("[VAD] %s\n", e.Type)

        case *vango.TranscriptDeltaEvent:
            fmt.Printf("[User] %s", e.Delta)

        case *vango.InputCommittedEvent:
            fmt.Printf("\n[Committed] %s\n", e.Transcript)

        case *vango.ContentBlockDeltaEvent:
            if text, ok := e.Delta.(*vango.TextDelta); ok {
                fmt.Printf("[Assistant] %s", text.Text)
            }

        case *vango.AudioChunkEvent:
            speaker.Write(e.Data)

        case *vango.InterruptDetectingEvent:
            fmt.Printf("[Interrupt?] %s\n", e.Transcript)

        case *vango.InterruptDismissedEvent:
            fmt.Printf("[Backchannel] %s (%s)\n", e.Transcript, e.Reason)

        case *vango.ResponseInterruptedEvent:
            fmt.Printf("[Interrupted] at %dms: %s\n", e.AudioPositionMs, e.InterruptTranscript)

        case *vango.ErrorEvent:
            log.Printf("[Error] %s: %s", e.Code, e.Message)
        }
    }
}()

// Send audio from microphone
for chunk := range microphone.Chunks() {
    session.SendAudio(chunk)
}
```

### 13.5 Mid-Session Configuration Updates

```go
// Switch to a different model mid-conversation
session.UpdateConfig(&vango.LiveConfig{
    Model: "openai/gpt-4o",
})

// Change voice settings
session.UpdateConfig(&vango.LiveConfig{
    Voice: &vango.VoiceConfig{
        Output: &vango.VoiceOutputConfig{
            Voice: "different-voice-id",
            Speed: 1.2,
        },
    },
})

// Add new tools
session.UpdateConfig(&vango.LiveConfig{
    Tools: []vango.Tool{
        vango.WebSearch(),
        newCustomTool,
    },
})
```

### 13.6 RunStream Integration

Live mode internally uses `RunStream` for agent execution, leveraging its existing interrupt support:

```go
// RunStream's Interrupt method is used when user interrupts during bot speech
func (rs *RunStream) Interrupt(msg Message, behavior InterruptBehavior) error

type InterruptBehavior int
const (
    InterruptDiscard    InterruptBehavior = iota // Don't save partial response
    InterruptSavePartial                          // Save as-is
    InterruptSaveMarked                           // Save with [interrupted] marker
)
```

This means the same tool execution loop, multi-turn state management, and interrupt handling works identically whether you're using `Messages.Run()` or `Messages.Live()`.

### 13.7 Audio Format Requirements

| Parameter | Value | Notes |
|-----------|-------|-------|
| **Encoding** | PCM signed 16-bit | Little-endian |
| **Sample Rate** | 24000 Hz | Configurable: 16000, 24000, 48000 |
| **Channels** | 1 (mono) | Required |
| **Chunk Size** | 4096 bytes | ~85ms at 24kHz |

---

## 14. Error Handling

### 14.1 Error Types

```go
type Error struct {
    Type          string `json:"type"`
    Message       string `json:"message"`
    Param         string `json:"param,omitempty"`
    Code          string `json:"code,omitempty"`
    RequestID     string `json:"request_id,omitempty"`
    ProviderError any    `json:"provider_error,omitempty"`
    RetryAfter    *int   `json:"retry_after,omitempty"`
}

func (e *Error) Error() string

// Error type constants
const (
    ErrInvalidRequest     = "invalid_request_error"
    ErrAuthentication     = "authentication_error"
    ErrPermission         = "permission_error"
    ErrNotFound           = "not_found_error"
    ErrRateLimit          = "rate_limit_error"
    ErrAPI                = "api_error"
    ErrOverloaded         = "overloaded_error"
    ErrProvider           = "provider_error"
)
```

### 14.2 Error Handling

```go
resp, err := client.Messages.Create(ctx, req)
if err != nil {
    var apiErr *vango.Error
    if errors.As(err, &apiErr) {
        switch apiErr.Type {
        case vango.ErrRateLimit:
            time.Sleep(30 * time.Second)
            // Retry...
        case vango.ErrAuthentication:
            log.Fatal("Invalid API key")
        case vango.ErrProvider:
            // Try different provider
            req.Model = "openai/gpt-4o"
            resp, err = client.Messages.Create(ctx, req)
        default:
            return fmt.Errorf("API error: %w", err)
        }
    }
    return err
}
```

### 14.3 Retry Behavior

```go
client := vango.NewClient(
    vango.WithRetries(3),
    vango.WithRetryBackoff(time.Second),
    vango.WithRetryOn(vango.ErrRateLimit, vango.ErrOverloaded),
)
```

---

## 15. Observability

### 15.1 Logging

```go
client := vango.NewClient(
    vango.WithLogger(slog.Default()),
    vango.WithLogLevel(slog.LevelDebug),
)

// Logs:
// level=INFO msg="request started" model=anthropic/claude-sonnet-4 messages=3
// level=DEBUG msg="request body" body={...}
// level=INFO msg="request complete" duration_ms=2341 tokens=2370 cost_usd=0.0142
```

### 15.2 OpenTelemetry Tracing

```go
client := vango.NewClient(
    vango.WithTracer(otel.Tracer("my-app")),
)

// Creates spans:
// my-app.vango.messages.create
//   └── http.request POST api.vango.dev/v1/messages
```

### 15.3 Metrics

```go
client := vango.NewClient(
    vango.WithMeter(otel.Meter("my-app")),
)

// Records:
// vango.requests{model, status}
// vango.tokens{model, direction}
// vango.duration{model}
// vango.cost{model}
```

### 15.4 Hooks

```go
client := vango.NewClient(
    vango.WithBeforeRequest(func(req *http.Request) {
        // Inspect/modify request
    }),
    vango.WithAfterResponse(func(resp *http.Response) {
        // Inspect response
        log.Printf("X-Request-ID: %s", resp.Header.Get("X-Request-ID"))
    }),
)
```

---

## 16. Testing

### 16.1 Mock Client

```go
mockClient := vango.NewMockClient()

mockClient.Messages.On("Create", mock.Anything, mock.Anything).Return(&vango.Response{
    Content: []vango.ContentBlock{
        &vango.TextBlock{Text: "Hello!"},
    },
    Usage: vango.Usage{InputTokens: 10, OutputTokens: 5},
}, nil)

// Use in tests
resp, err := mockClient.Messages.Create(ctx, req)
```

### 16.2 Recording/Playback

```go
// Record mode
client := vango.NewClient(
    vango.WithRecorder("testdata/messages"),
)

// Playback mode
client := vango.NewClient(
    vango.WithPlayback("testdata/messages"),
)
```

### 16.3 Test Helpers

```go
func TestMyAgent(t *testing.T) {
    client := vango.NewTestClient(t,
        vango.WithResponse(&vango.Response{...}),
        vango.WithStreamEvents([]vango.StreamEvent{...}),
    )
    
    // Run tests with predictable responses
}
```

---

## 17. Examples

### 17.1 Simple Chat

```go
package main

import (
    "context"
    "fmt"
    "github.com/vango-ai/vango-go"
)

func main() {
    client := vango.NewClient()
    
    resp, err := client.Messages.Create(context.Background(), &vango.MessageRequest{
        Model: "anthropic/claude-sonnet-4",
        Messages: []vango.Message{
            {Role: "user", Content: vango.Text("What is the capital of France?")},
        },
    })
    if err != nil {
        panic(err)
    }
    
    fmt.Println(resp.TextContent())
}
```

### 17.2 Streaming Chat

```go
stream, _ := client.Messages.Stream(ctx, req)
defer stream.Close()

for event := range stream.Events() {
    if delta, ok := event.(*vango.ContentBlockDeltaEvent); ok {
        if text, ok := delta.Delta.(*vango.TextDelta); ok {
            fmt.Print(text.Text)
        }
    }
}
```

### 17.3 Research Agent

```go
result, _ := client.Messages.Run(ctx, &vango.MessageRequest{
    Model:  "anthropic/claude-sonnet-4",
    System: "You are a research assistant. Search thoroughly and cite sources.",
    Messages: []vango.Message{
        {Role: "user", Content: vango.Text("What are the latest developments in fusion energy?")},
    },
    Tools: []vango.Tool{vango.WebSearch()},
},
    vango.WithMaxToolCalls(10),
    vango.WithOnToolCall(func(name string, input, output any, err error) {
        fmt.Printf("[%s] %v\n", name, input)
    }),
)

fmt.Println(result.Response.TextContent())
```

### 17.4 Voice Assistant

```go
conv := vango.NewConversation(client,
    vango.ConvModel("anthropic/claude-sonnet-4"),
    vango.ConvVoice(&vango.VoiceConfig{
        Input:  &vango.VoiceInputConfig{Provider: "deepgram"},
        Output: &vango.VoiceOutputConfig{Provider: "elevenlabs", Voice: "rachel"},
    }),
)

for {
    audio := recordFromMicrophone()
    
    stream, _ := conv.Stream(ctx, vango.AudioInput(audio))
    
    for event := range stream.Events() {
        switch e := event.(type) {
        case *vango.TranscriptDeltaEvent:
            if e.Role == "user" {
                fmt.Printf("You: %s", e.Delta.Text)
            }
        case *vango.AudioDeltaEvent:
            playAudio(e.Delta.Data)
        }
    }
}
```

### 17.5 Structured Data Extraction

```go
// 17.5 Structured Data Extraction (The Easy Way)
type Entity struct {
    Person  string `json:"person" desc:"Name of the person"`
    Role    string `json:"role" desc:"Job title or position"`
    Company string `json:"company" desc:"Organization name"`
}

func main() {
    client := vango.NewClient()
    
    // The struct is all you need. The SDK handles schema generation + validation.
    var output Entity
    
    resp, err := client.Messages.Extract(context.Background(), &vango.MessageRequest{
        Model: "anthropic/claude-sonnet-4", // Uses native Anthropic structured outputs
        Messages: []vango.Message{
            {Role: "user", Content: vango.Text("Tim Cook is CEO of Apple Inc.")},
        },
    }, &output)
    
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("%s works at %s as %s\n", output.Person, output.Company, output.Role)
    // Prints: Tim Cook works at Apple Inc. as CEO
}
```

---

## Appendix A: Type Reference

See `API_SPEC.md` for complete type definitions shared between API and SDK.

---

## Appendix B: Migration from GAI v1

```go
// Before (GAI v1)
import "github.com/shillcollin/gai/providers/anthropic"
client := anthropic.New(anthropic.WithAPIKey(key))
result, _ := client.GenerateText(ctx, core.Request{...})

// After (Vango)
import "github.com/vango-ai/vango-go"
client := vango.NewClient()
resp, _ := client.Messages.Create(ctx, &vango.MessageRequest{...})
```

---

*Document Version: 1.0.0*  
*Last Updated: December 2025*  
*Status: Implementation Ready*

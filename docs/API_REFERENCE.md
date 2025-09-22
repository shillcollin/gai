# gai API Reference

Complete reference documentation for the gai Go AI SDK, covering all core types, interfaces, and functions.

## Table of Contents

1. [Core Package](#core-package)
   - [Provider Interface](#provider-interface)
   - [Request & Response Types](#request--response-types)
   - [Messages & Parts](#messages--parts)
   - [Streaming Types](#streaming-types)
   - [Error Types](#error-types)
2. [Tools Package](#tools-package)
   - [Tool Definition](#tool-definition)
   - [Tool Execution](#tool-execution)
3. [Runner Package](#runner-package)
   - [Runner Configuration](#runner-configuration)
   - [Stop Conditions](#stop-conditions)
   - [Finalizers](#finalizers)
4. [Prompts Package](#prompts-package)
5. [Observability Package](#observability-package)
6. [Stream Package](#stream-package)
7. [Middleware Package](#middleware-package)
8. [Provider Adapters](#provider-adapters)

## Core Package

The `core` package defines all fundamental types and interfaces for the SDK. These types are provider-agnostic and ensure consistent behavior across all AI providers.

### Provider Interface

The central interface that all provider adapters must implement:

```go
package core

import (
    "context"
)

// Provider is the primary interface for AI model providers
type Provider interface {
    // GenerateText generates a text response for the given request
    GenerateText(ctx context.Context, req Request) (*TextResult, error)

    // StreamText generates a streaming text response
    StreamText(ctx context.Context, req Request) (*Stream, error)

    // GenerateObject generates a structured JSON response (raw)
    GenerateObject(ctx context.Context, req Request) (*ObjectResultRaw, error)

    // StreamObject generates a streaming structured JSON response (raw)
    StreamObject(ctx context.Context, req Request) (*ObjectStreamRaw, error)

    // Capabilities returns the provider's capabilities
    Capabilities() Capabilities
}

// Capabilities describes what features a provider supports
type Capabilities struct {
    // Core capabilities
    Streaming         bool // Supports streaming responses
    ParallelToolCalls bool // Can call multiple tools in one step
    StrictJSON        bool // Supports strict JSON mode

    // Multimodal capabilities
    Images bool // Can process images
    Audio  bool // Can process audio
    Video  bool // Can process video
    Files  bool // Can process documents

    // Advanced capabilities
    Reasoning  bool // Supports reasoning/thinking models
    Citations  bool // Can provide source citations
    Safety     bool // Has safety filtering
    Sessions   bool // Supports session caching

    // Limits
    MaxInputTokens  int     // Maximum input context size
    MaxOutputTokens int     // Maximum output size
    MaxFileSize     int64   // Maximum file upload size in bytes
    MaxToolCalls    int     // Maximum tools per step

    // Metadata
    Provider string // Provider name (openai, anthropic, gemini)
    Models   []string // Available models
}
```

### Request & Response Types

Core types for making requests and receiving responses:

```go
// Request represents a generation request to any provider
type Request struct {
    // Model selection (optional, uses provider default if empty)
    Model string `json:"model,omitempty"`

    // Messages forming the conversation
    Messages []Message `json:"messages"`

    // Generation parameters
    Temperature float32 `json:"temperature,omitempty"` // 0.0 to 2.0
    MaxTokens   int     `json:"max_tokens,omitempty"`
    TopP        float32 `json:"top_p,omitempty"`        // 0.0 to 1.0
    TopK        int     `json:"top_k,omitempty"`

    // Streaming control
    Stream bool `json:"stream,omitempty"`

    // Safety configuration
    Safety *SafetyConfig `json:"safety,omitempty"`

    // Session for caching
    Session *Session `json:"session,omitempty"`

    // Tools configuration
    Tools      []ToolHandle `json:"tools,omitempty"`
    ToolChoice ToolChoice   `json:"tool_choice,omitempty"`

    // Provider-specific options
    ProviderOptions map[string]any `json:"provider_options,omitempty"`

    // Metadata for observability
    Metadata map[string]any `json:"metadata,omitempty"`
}

// Warning communicates a non-fatal adjustment that occurred while processing a request.
type Warning struct {
    Code    string `json:"code"`
    Field   string `json:"field,omitempty"`
    Message string `json:"message"`
}

// TextResult represents a non-streaming text generation result
type TextResult struct {
    Text         string        `json:"text"`
    Model        string        `json:"model"`
    Provider     string        `json:"provider"`
    Usage        Usage         `json:"usage"`
    Steps        []Step        `json:"steps,omitempty"`    // For multi-step execution
    Citations    []Citation    `json:"citations,omitempty"`
    Safety       []SafetyEvent `json:"safety,omitempty"`
    FinishReason StopReason    `json:"finish_reason"`
    LatencyMS    int64         `json:"latency_ms,omitempty"`
    TTFBMS       int64         `json:"ttfb_ms,omitempty"`
    Warnings     []Warning     `json:"warnings,omitempty"`
}

// ObjectResultRaw represents raw JSON generation result
type ObjectResultRaw struct {
    JSON     []byte    `json:"json"`
    Model    string    `json:"model"`
    Provider string    `json:"provider"`
    Usage    Usage     `json:"usage"`
    Warnings []Warning `json:"warnings,omitempty"`
}

// Usage tracks token consumption and costs
type Usage struct {
    InputTokens     int     `json:"input_tokens"`
    OutputTokens    int     `json:"output_tokens"`
    TotalTokens     int     `json:"total_tokens"`
    ReasoningTokens int     `json:"reasoning_tokens,omitempty"` // For thinking models
    CostUSD         float64 `json:"cost_usd,omitempty"`

    // Detailed breakdown (optional)
    CachedInputTokens int `json:"cached_input_tokens,omitempty"`
    AudioTokens       int `json:"audio_tokens,omitempty"`
}

// StopReason describes why generation stopped
type StopReason struct {
    Type        string         `json:"type"` // stop, length, content_filter, tool_calls_exhausted, max_steps, error
    Description string         `json:"description,omitempty"`
    Details     map[string]any `json:"details,omitempty"`
}
```

### Messages & Parts

The multimodal message system supporting various content types:

```go
// Role represents the sender of a message
type Role string

const (
    System    Role = "system"
    User      Role = "user"
    Assistant Role = "assistant"
)

// Message represents a single message in a conversation
type Message struct {
    Role  Role   `json:"role"`
    Parts []Part `json:"parts"` // Multimodal parts

    // Optional metadata
    Name     string         `json:"name,omitempty"`     // For named participants
    Metadata map[string]any `json:"metadata,omitempty"`
}

// Part is the interface for message content
type Part interface {
    Type() PartType
    Content() any
}

// PartType identifies the type of content
type PartType string

const (
    PartTypeText       PartType = "text"
    PartTypeImage      PartType = "image"
    PartTypeImageURL   PartType = "image_url"
    PartTypeAudio      PartType = "audio"
    PartTypeVideo      PartType = "video"
    PartTypeFile       PartType = "file"
    PartTypeToolCall   PartType = "tool_call"
    PartTypeToolResult PartType = "tool_result"
)

// Text represents text content
type Text struct {
    Text string `json:"text"`
}

func (t Text) Type() PartType    { return PartTypeText }
func (t Text) Content() any      { return t.Text }

// Image represents image content from bytes
type Image struct {
    Source BlobRef `json:"source"`
    Alt    string  `json:"alt,omitempty"` // Alternative text
}

func (i Image) Type() PartType   { return PartTypeImage }
func (i Image) Content() any     { return i.Source }

// ImageURL represents image content from a URL
type ImageURL struct {
    URL    string `json:"url"`
    MIME   string `json:"mime,omitempty"`
    Alt    string `json:"alt,omitempty"`
    Detail string `json:"detail,omitempty"` // high, low, auto
}

func (i ImageURL) Type() PartType { return PartTypeImageURL }
func (i ImageURL) Content() any   { return i.URL }

// Audio represents audio content
type Audio struct {
    Source   BlobRef       `json:"source"`
    Format   string        `json:"format,omitempty"`   // mp3, wav, etc.
    Duration time.Duration `json:"duration,omitempty"`
}

func (a Audio) Type() PartType    { return PartTypeAudio }
func (a Audio) Content() any      { return a.Source }

// Video represents video content
type Video struct {
    Source   BlobRef       `json:"source"`
    Format   string        `json:"format,omitempty"`
    Duration time.Duration `json:"duration,omitempty"`
}

func (v Video) Type() PartType    { return PartTypeVideo }
func (v Video) Content() any      { return v.Source }

// File represents document content
type File struct {
    Source BlobRef `json:"source"`
    Name   string  `json:"name,omitempty"`
}

func (f File) Type() PartType     { return PartTypeFile }
func (f File) Content() any       { return f.Source }

// ToolCall represents a tool invocation by the model
type ToolCall struct {
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input"`
}

func (t ToolCall) Type() PartType { return PartTypeToolCall }
func (t ToolCall) Content() any   { return t }

// ToolResult represents the result of a tool execution
type ToolResult struct {
    ID     string `json:"id"`     // Matches ToolCall.ID
    Name   string `json:"name"`
    Result any    `json:"result"`
    Error  error  `json:"error,omitempty"`
}

func (t ToolResult) Type() PartType { return PartTypeToolResult }
func (t ToolResult) Content() any   { return t }
```

### BlobRef System

For handling binary data efficiently:

```go
// BlobRef represents a reference to binary data
type BlobRef struct {
    Kind BlobKind `json:"kind"`

    // One of these will be set based on Kind
    Bytes      []byte `json:"bytes,omitempty"`
    Path       string `json:"path,omitempty"`
    URL        string `json:"url,omitempty"`
    ProviderID string `json:"provider_id,omitempty"` // Provider-specific file ID

    // Metadata
    MIME string `json:"mime"`
    Size int64  `json:"size,omitempty"`
    Hash string `json:"hash,omitempty"` // SHA256
}

// BlobKind identifies how the blob is stored
type BlobKind string

const (
    BlobBytes    BlobKind = "bytes"    // In-memory bytes
    BlobPath     BlobKind = "path"     // File path
    BlobURL      BlobKind = "url"      // HTTP(S) URL
    BlobProvider BlobKind = "provider" // Provider-managed
)

// Methods for BlobRef
func (b *BlobRef) Read() ([]byte, error)
func (b *BlobRef) Stream() (io.ReadCloser, error)
func (b *BlobRef) Validate() error
```

### Streaming Types

Types for handling streaming responses:

```go
// Stream represents a streaming response
type Stream struct {
    events  <-chan StreamEvent
    cancel  context.CancelFunc
    err     error
    mu      sync.RWMutex
    closed  bool
}

// Stream methods
func (s *Stream) Events() <-chan StreamEvent
func (s *Stream) Close() error
func (s *Stream) Err() error

// StreamEvent represents a single event in a stream
type StreamEvent struct {
    Type      EventType      `json:"type"`
    Schema    string         `json:"schema"` // "gai.events.v1"
    Seq       int            `json:"seq"`    // Sequence number
    Timestamp time.Time      `json:"ts"`
    StreamID  string         `json:"stream_id"`

    // Event-specific data (based on Type)
    TextDelta        string           `json:"text,omitempty"`
    ReasoningDelta   string           `json:"reasoning,omitempty"`
    ReasoningSummary ReasoningSummary `json:"reasoning_summary,omitempty"`
    ToolCall         ToolCall         `json:"tool_call,omitempty"`
    ToolResult       ToolResult       `json:"tool_result,omitempty"`
    Citations        []Citation       `json:"citations,omitempty"`
    Safety           SafetyEvent      `json:"safety,omitempty"`
    Usage            Usage            `json:"usage,omitempty"`
    FinishReason     StopReason       `json:"finish_reason,omitempty"`
    Error            error            `json:"error,omitempty"`

    // Step tracking
    StepID     int    `json:"step_id,omitempty"`
    DurationMS int64  `json:"duration_ms,omitempty"`
    ToolCalls  int    `json:"tool_calls,omitempty"`

    // Policies and capabilities (start event only)
    Capabilities []string          `json:"capabilities,omitempty"`
    Policies     StreamingPolicies `json:"policies,omitempty"`

    // Provider-specific extensions
    Ext map[string]any `json:"ext,omitempty"`
}

// EventType identifies the type of stream event
type EventType string

const (
    EventStart            EventType = "start"
    EventTextDelta        EventType = "text.delta"
    EventReasoningDelta   EventType = "reasoning.delta"
    EventReasoningSummary EventType = "reasoning.summary"
    EventAudioDelta       EventType = "audio.delta"
    EventToolCall         EventType = "tool.call"
    EventToolResult       EventType = "tool.result"
    EventCitations        EventType = "citations"
    EventSafety           EventType = "safety"
    EventStepStart        EventType = "step.start"
    EventStepFinish       EventType = "step.finish"
    EventFinish           EventType = "finish"
    EventError            EventType = "error"
)

// StreamingPolicies defines server policies for the stream
type StreamingPolicies struct {
    ReasoningVisibility string `json:"reasoning_visibility"` // none, summary, raw
    MaskErrors          bool   `json:"mask_errors"`
    SendStart           bool   `json:"send_start"`
    SendFinish          bool   `json:"send_finish"`
}
```

### Safety Configuration

Types for configuring and handling safety features:

```go
// SafetyConfig configures content safety thresholds
type SafetyConfig struct {
    Harassment SafetyLevel `json:"harassment,omitempty"`
    Hate       SafetyLevel `json:"hate,omitempty"`
    Sexual     SafetyLevel `json:"sexual,omitempty"`
    Dangerous  SafetyLevel `json:"dangerous,omitempty"`
    SelfHarm   SafetyLevel `json:"self_harm,omitempty"`
    Other      SafetyLevel `json:"other,omitempty"`
}

// SafetyLevel defines filtering thresholds
type SafetyLevel string

const (
    SafetyNone   SafetyLevel = "none"   // No filtering
    SafetyLow    SafetyLevel = "low"    // Minimal filtering
    SafetyMedium SafetyLevel = "medium" // Balanced filtering
    SafetyHigh   SafetyLevel = "high"   // Strict filtering
    SafetyBlock  SafetyLevel = "block"  // Block all
)

// SafetyEvent represents a safety-related occurrence
type SafetyEvent struct {
    Category string      `json:"category"`
    Action   string      `json:"action"` // allow, block, redact, safe_completion
    Score    float32     `json:"score"`  // 0.0 to 1.0
    Target   string      `json:"target"` // input or output
    Start    int         `json:"start,omitempty"` // Character offset
    End      int         `json:"end,omitempty"`
}
```

### Citation Support

For models that provide source citations:

```go
// Citation represents a source reference
type Citation struct {
    URI     string  `json:"uri"`
    Title   string  `json:"title,omitempty"`
    Snippet string  `json:"snippet,omitempty"`
    Start   int     `json:"start,omitempty"` // Character offset in text
    End     int     `json:"end,omitempty"`
    Score   float32 `json:"score,omitempty"` // Relevance score
}
```

### Session Management

For caching and context reuse:

```go
// Session represents a cached conversation context
type Session struct {
    Provider string `json:"provider"`
    ID       string `json:"id"`

    // Optional metadata
    CreatedAt time.Time      `json:"created_at,omitempty"`
    ExpiresAt time.Time      `json:"expires_at,omitempty"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}
```

### Error Types

Comprehensive error taxonomy for consistent error handling:

```go
// AIError represents a categorized error from the SDK
type AIError struct {
    Code       ErrorCode      `json:"code"`
    Message    string         `json:"message"`
    Status     int            `json:"status,omitempty"` // HTTP status if applicable
    Retryable  bool           `json:"retryable"`
    Details    map[string]any `json:"details,omitempty"`
    RetryAfter time.Duration  `json:"retry_after,omitempty"`
    wrapped    error
}

// ErrorCode categorizes errors for handling
type ErrorCode string

const (
    ErrRateLimited      ErrorCode = "rate_limited"
    ErrContentFiltered  ErrorCode = "content_filtered"
    ErrBadRequest       ErrorCode = "bad_request"
    ErrTransient        ErrorCode = "transient"
    ErrProviderError    ErrorCode = "provider_error"
    ErrTimeout          ErrorCode = "timeout"
    ErrCanceled         ErrorCode = "canceled"
    ErrToolError        ErrorCode = "tool_error"
    ErrInternal         ErrorCode = "internal"
)

// Error helper functions
func IsRateLimited(err error) bool
func IsContentFiltered(err error) bool
func IsTransient(err error) bool
func IsBadRequest(err error) bool
func IsTimeout(err error) bool
func IsCanceled(err error) bool
func IsToolError(err error) bool
func GetRetryAfter(err error) time.Duration
func WrapError(err error, code ErrorCode) *AIError
```

## Tools Package

The `tools` package provides strongly-typed tool definitions with automatic JSON Schema generation.

### Tool Definition

Creating typed tools with input/output validation:

```go
package tools

import (
    "context"
    "github.com/shillcollin/gai/core"
)

// Tool represents a typed tool with input and output types
type Tool[I any, O any] struct {
    name        string
    description string
    fn          ToolFunc[I, O]
    inputSchema  *JSONSchema
    outputSchema *JSONSchema
}

// ToolFunc is the function signature for tool implementations
type ToolFunc[I any, O any] func(ctx context.Context, input I, meta Meta) (O, error)

// Meta provides tool invocation metadata
type Meta struct {
    CallID   string         // Unique call identifier
    StepID   int            // Step number in multi-step execution
    TraceID  string         // Distributed trace ID
    SpanID   string         // Span ID for observability
    Metadata map[string]any // Additional metadata
}

// New creates a new typed tool
func New[I any, O any](name, description string, fn ToolFunc[I, O]) *Tool[I, O] {
    t := &Tool[I, O]{
        name:        name,
        description: description,
        fn:          fn,
    }

    // Derive JSON schemas from types
    t.inputSchema = deriveSchema[I]()
    t.outputSchema = deriveSchema[O]()

    return t
}

// Tool methods
func (t *Tool[I, O]) Name() string
func (t *Tool[I, O]) Description() string
func (t *Tool[I, O]) InputSchema() *JSONSchema
func (t *Tool[I, O]) OutputSchema() *JSONSchema
func (t *Tool[I, O]) Execute(ctx context.Context, input map[string]any, meta Meta) (any, error)

// CoreAdapter adapts a typed tool to core.ToolHandle
type CoreAdapter[I any, O any] struct {
    tool *Tool[I, O]
}

func NewCoreAdapter[I any, O any](tool *Tool[I, O]) core.ToolHandle {
    return &CoreAdapter[I, O]{tool: tool}
}
```

### JSON Schema Generation

Automatic schema derivation from Go types:

```go
// JSONSchema represents a JSON Schema definition
type JSONSchema struct {
    Type        string                    `json:"type,omitempty"`
    Properties  map[string]*JSONSchema    `json:"properties,omitempty"`
    Required    []string                  `json:"required,omitempty"`
    Items       *JSONSchema               `json:"items,omitempty"`
    Description string                    `json:"description,omitempty"`
    Default     any                       `json:"default,omitempty"`
    Enum        []any                     `json:"enum,omitempty"`
    Minimum     *float64                  `json:"minimum,omitempty"`
    Maximum     *float64                  `json:"maximum,omitempty"`
    MinLength   *int                      `json:"minLength,omitempty"`
    MaxLength   *int                      `json:"maxLength,omitempty"`
    Pattern     string                    `json:"pattern,omitempty"`
    Format      string                    `json:"format,omitempty"`
}

// Struct tag support for schema customization
type Example struct {
    Name        string   `json:"name" description:"User's name"`
    Age         int      `json:"age" minimum:"0" maximum:"150"`
    Email       string   `json:"email" format:"email"`
    Tags        []string `json:"tags,omitempty"`
    Active      bool     `json:"active" default:"true"`
    Role        string   `json:"role" enum:"admin,user,guest"`
    Description string   `json:"description" maxLength:"500"`
}
```

## Runner Package

The `runner` package orchestrates multi-step tool execution with sophisticated control flow.

### Runner Configuration

Creating and configuring a runner:

```go
package runner

import (
    "context"
    "time"
    "github.com/shillcollin/gai/core"
)

// Runner orchestrates multi-step execution with tools
type Runner struct {
    provider     core.Provider
    maxParallel  int
    toolTimeout  time.Duration
    errorMode    ToolErrorMode
    retry        ToolRetry
    memo         ToolMemo
    interceptors []Interceptor
}

// RunnerOption configures the runner
type RunnerOption func(*Runner)

// NewRunner creates a new runner with options
func NewRunner(p core.Provider, opts ...RunnerOption) *Runner {
    r := &Runner{
        provider:    p,
        maxParallel: 5,
        toolTimeout: 30 * time.Second,
        errorMode:   ToolErrorPropagate,
    }

    for _, opt := range opts {
        opt(r)
    }

    return r
}

// Configuration options
func WithMaxParallel(n int) RunnerOption
func WithToolTimeout(d time.Duration) RunnerOption
func WithOnToolError(mode ToolErrorMode) RunnerOption
func WithToolRetry(r ToolRetry) RunnerOption
func WithMemo(m ToolMemo) RunnerOption
func WithInterceptor(i Interceptor) RunnerOption

// ToolErrorMode defines how tool errors are handled
type ToolErrorMode int

const (
    ToolErrorPropagate ToolErrorMode = iota // Stop execution on error
    ToolErrorAppendAndContinue              // Include error in transcript and continue
)

// ToolRetry configures tool retry behavior
type ToolRetry struct {
    MaxAttempts int           // Maximum retry attempts
    BaseDelay   time.Duration // Initial delay between retries
    Jitter      bool          // Add randomization to delays
    MaxDelay    time.Duration // Maximum delay between retries
}

// ToolMemo provides tool result caching
type ToolMemo interface {
    Get(name string, inputHash string) (any, bool)
    Set(name string, inputHash string, result any)
}
```

### Execution Methods

Running requests with tools:

```go
// ExecuteRequest runs a request to completion with tools
func (r *Runner) ExecuteRequest(
    ctx context.Context,
    req core.Request,
) (*core.TextResult, error) {
    // Set default stop condition if none provided
    if req.StopWhen == nil {
        req.StopWhen = core.NoMoreTools()
    }

    state := &State{
        Messages: req.Messages,
        Steps:    []core.Step{},
        Usage:    core.Usage{},
    }

    for stepNum := 1; ; stepNum++ {
        // Check stop conditions
        if stop, reason := req.StopWhen(state); stop {
            state.StopReason = reason
            break
        }

        // Execute next step
        step, err := r.executeStep(ctx, req, state, stepNum)
        if err != nil {
            return nil, err
        }

        state.Steps = append(state.Steps, step)
        state.LastStep = &step

        // Update usage
        state.Usage.InputTokens += step.Usage.InputTokens
        state.Usage.OutputTokens += step.Usage.OutputTokens
        state.Usage.TotalTokens += step.Usage.TotalTokens
        state.Usage.CostUSD += step.Usage.CostUSD
    }

    // Run finalizer if provided
    if req.OnStop != nil {
        return r.runFinalizer(ctx, req.OnStop, state)
    }

    return &core.TextResult{
        Text:  state.LastText(),
        Steps: state.Steps,
        Usage: state.Usage,
        FinishReason: state.StopReason,
    }, nil
}

// StreamRequest runs a request with streaming
func (r *Runner) StreamRequest(
    ctx context.Context,
    req core.Request,
) (*core.Stream, error) {
    // Implementation for streaming execution
}
```

### Step Execution

Internal step execution logic:

```go
// Step represents a single execution step
type Step struct {
    Number      int
    Text        string
    ToolCalls   []ToolExecution
    Usage       core.Usage
    DurationMS  int64
    StartedAt   time.Time
    CompletedAt time.Time
}

// ToolExecution represents a tool call and its result
type ToolExecution struct {
    Call      core.ToolCall
    Result    any
    Error     error
    DurationMS int64
    Retries   int
}

// State tracks execution state
type State struct {
    Messages   []core.Message
    Steps      []Step
    LastStep   *Step
    Usage      core.Usage
    StopReason core.StopReason
}

// State methods
func (s *State) LastText() string
func (s *State) TotalSteps() int
func (s *State) TotalToolCalls() int
```

### Stop Conditions

Built-in and custom stop conditions:

```go
// StopCondition determines when to stop execution
type StopCondition func(*State) (stop bool, reason core.StopReason)

// Built-in conditions
func MaxSteps(n int) StopCondition {
    return func(s *State) (bool, core.StopReason) {
        if s.TotalSteps() >= n {
            return true, core.StopReason{
                Type: "max_steps",
                Description: fmt.Sprintf("Reached maximum of %d steps", n),
            }
        }
        return false, core.StopReason{}
    }
}

func NoMoreTools() StopCondition {
    return func(s *State) (bool, core.StopReason) {
        if s.LastStep != nil && len(s.LastStep.ToolCalls) == 0 {
            return true, core.StopReason{
                Type: "no_more_tools",
                Description: "No tool calls in last step",
            }
        }
        return false, core.StopReason{}
    }
}

func UntilToolSeen(name string) StopCondition {
    return func(s *State) (bool, core.StopReason) {
        for _, step := range s.Steps {
            for _, tc := range step.ToolCalls {
                if tc.Call.Name == name {
                    return true, core.StopReason{
                        Type: "tool_seen",
                        Description: fmt.Sprintf("Tool %s was called", name),
                    }
                }
            }
        }
        return false, core.StopReason{}
    }
}

func UntilTextLength(n int) StopCondition {
    return func(s *State) (bool, core.StopReason) {
        total := 0
        for _, step := range s.Steps {
            total += len(step.Text)
        }
        if total >= n {
            return true, core.StopReason{
                Type: "text_length",
                Description: fmt.Sprintf("Text length reached %d", total),
            }
        }
        return false, core.StopReason{}
    }
}

// Combinators
func Any(conditions ...StopCondition) StopCondition {
    return func(s *State) (bool, core.StopReason) {
        for _, cond := range conditions {
            if stop, reason := cond(s); stop {
                return true, reason
            }
        }
        return false, core.StopReason{}
    }
}

func All(conditions ...StopCondition) StopCondition {
    return func(s *State) (bool, core.StopReason) {
        reasons := []string{}
        for _, cond := range conditions {
            if stop, reason := cond(s); !stop {
                return false, core.StopReason{}
            } else {
                reasons = append(reasons, reason.Type)
            }
        }
        return true, core.StopReason{
            Type: "all_conditions_met",
            Description: fmt.Sprintf("All conditions met: %v", reasons),
        }
    }
}

func CombineConditions(conditions ...StopCondition) StopCondition {
    return Any(conditions...)
}
```

### Finalizers

Post-processing after execution completes:

```go
// FinalState provides execution summary for finalizers
type FinalState struct {
    Messages    []core.Message
    Steps       []Step
    Usage       core.Usage
    StopReason  core.StopReason
    LastText    func() string
    TotalTokens func() int
}

// OnStop is a finalizer function
type OnStop func(ctx context.Context, state FinalState) (*core.TextResult, error)

// Example finalizers
func SummarizingFinalizer(summaryPrompt string) OnStop {
    return func(ctx context.Context, state FinalState) (*core.TextResult, error) {
        // Generate summary of execution
        summary := fmt.Sprintf(
            "Completed %d steps with %d tool calls. Final status: %s",
            len(state.Steps),
            countToolCalls(state.Steps),
            state.StopReason.Type,
        )

        return &core.TextResult{
            Text: fmt.Sprintf("%s\n\nSummary: %s", state.LastText(), summary),
            Steps: state.Steps,
            Usage: state.Usage,
        }, nil
    }
}

func ValidationFinalizer(validator func(string) error) OnStop {
    return func(ctx context.Context, state FinalState) (*core.TextResult, error) {
        if err := validator(state.LastText()); err != nil {
            return nil, fmt.Errorf("validation failed: %w", err)
        }

        return &core.TextResult{
            Text: state.LastText(),
            Steps: state.Steps,
            Usage: state.Usage,
        }, nil
    }
}
```

## Prompts Package

The `prompts` package manages versioned prompt templates with hot reloading.

```go
package prompts

import (
    "context"
    "io/fs"
    "text/template"
)

// Registry manages prompt templates
type Registry struct {
    fs           fs.FS
    overrideDir  string
    templates    map[string]map[string]*template.Template
    helpers      template.FuncMap
    mu           sync.RWMutex
}

// NewRegistry creates a prompt registry
func NewRegistry(promptFS fs.FS, opts ...RegistryOption) *Registry {
    r := &Registry{
        fs:        promptFS,
        templates: make(map[string]map[string]*template.Template),
        helpers:   make(template.FuncMap),
    }

    for _, opt := range opts {
        opt(r)
    }

    return r
}

// Registry options
type RegistryOption func(*Registry)

func WithOverrideDir(dir string) RegistryOption
func WithHelperFunc(name string, fn any) RegistryOption
func WithHelpers(funcs template.FuncMap) RegistryOption

// Registry methods
func (r *Registry) Reload() error
func (r *Registry) Render(ctx context.Context, name, version string, data any) (string, PromptID, error)
func (r *Registry) ListVersions(name string) []string
func (r *Registry) HasOverride(name string) string
func (r *Registry) GetTemplate(name, version string) (*template.Template, error)

// PromptID identifies a specific prompt instance
type PromptID struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Fingerprint string `json:"fingerprint"` // Hash of template
}

// Versioning follows semantic versioning
// Template naming: name@version.tmpl
// Example: summarize@1.2.0.tmpl
```

## Observability Package

The `obs` package provides OpenTelemetry integration for comprehensive observability.

```go
package obs

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/metric"
)

// Options configures observability
type Options struct {
    Provider    string            // stdout, otlp, braintrust, phoenix
    ServiceName string
    Endpoint    string            // For OTLP
    Headers     map[string]string // Authentication
    SampleRate  float64           // 0.0 to 1.0

    // Export configuration
    BatchSize     int
    QueueSize     int
    ExportTimeout time.Duration
}

// Init initializes observability with the given options
func Init(opts Options) (shutdown func(), err error) {
    // Configure trace provider
    // Configure metric provider
    // Return shutdown function
}

// Tracer returns the global tracer
func Tracer() trace.Tracer {
    return otel.Tracer("github.com/shillcollin/gai")
}

// Meter returns the global meter
func Meter() metric.Meter {
    return otel.Meter("github.com/shillcollin/gai")
}

// Helper functions for common operations
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span)
func RecordTokens(span trace.Span, input, output int)
func RecordCost(span trace.Span, costUSD float64)
func RecordLatency(span trace.Span, latencyMS int64)
func RecordError(span trace.Span, err error)

// Automatic instrumentation for providers
func InstrumentProvider(p core.Provider) core.Provider {
    return &instrumentedProvider{
        Provider: p,
        tracer:   Tracer(),
        meter:    Meter(),
    }
}
```

## Stream Package

The `stream` package provides helpers for working with normalized event streams.

```go
package stream

import (
    "context"
    "io"
    "net/http"
)

// Writer interfaces for different stream formats
type Writer interface {
    Write(event core.StreamEvent) error
    Flush() error
    Close() error
}

// SSEWriter writes Server-Sent Events
type SSEWriter struct {
    w       io.Writer
    flusher http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) *SSEWriter
func (w *SSEWriter) Write(event core.StreamEvent) error
func (w *SSEWriter) Flush() error

// NDJSONWriter writes Newline Delimited JSON
type NDJSONWriter struct {
    w       io.Writer
    encoder *json.Encoder
}

func NewNDJSONWriter(w io.Writer) *NDJSONWriter
func (w *NDJSONWriter) Write(event core.StreamEvent) error

// Helpers for HTTP handlers
func SSE(w http.ResponseWriter, s *core.Stream) error
func NDJSON(w http.ResponseWriter, s *core.Stream) error
func SSEWithPolicy(w http.ResponseWriter, s *core.Stream, policy Policy) error

// Policy controls what events are sent
type Policy struct {
    SendStart      bool
    SendFinish     bool
    SendReasoning  bool
    SendSources    bool
    MaskErrors     bool
    BufferSize     int
}

// Reader for consuming streams
type Reader struct {
    source  io.ReadCloser
    format  Format
    decoder Decoder
}

func NewReader(r io.ReadCloser, format Format) *Reader
func (r *Reader) Read() (core.StreamEvent, error)
func (r *Reader) Close() error

type Format int

const (
    FormatSSE Format = iota
    FormatNDJSON
)
```

## Middleware Package

The `middleware` package provides composable provider middleware for cross-cutting concerns.

```go
package middleware

import (
    "context"
    "time"
    "github.com/shillcollin/gai/core"
)

// Middleware wraps a provider with additional functionality
type Middleware func(core.Provider) core.Provider

// Chain combines multiple middlewares
func Chain(middlewares ...Middleware) Middleware {
    return func(p core.Provider) core.Provider {
        for i := len(middlewares) - 1; i >= 0; i-- {
            p = middlewares[i](p)
        }
        return p
    }
}

// Retry middleware
func WithRetry(opts RetryOpts) Middleware {
    return func(p core.Provider) core.Provider {
        return &retryProvider{
            Provider: p,
            opts:     opts,
        }
    }
}

type RetryOpts struct {
    MaxAttempts    int
    BaseDelay      time.Duration
    MaxDelay       time.Duration
    Multiplier     float64
    Jitter         bool
    ShouldRetry    func(error, int) bool
    CalculateDelay func(error, int, time.Duration) time.Duration
}

// Rate limiting middleware
func WithRateLimit(rps int, burst int) Middleware {
    limiter := rate.NewLimiter(rate.Limit(rps), burst)
    return func(p core.Provider) core.Provider {
        return &rateLimitProvider{
            Provider: p,
            limiter:  limiter,
        }
    }
}

// Caching middleware
func WithCache(cache Cache, ttl time.Duration) Middleware {
    return func(p core.Provider) core.Provider {
        return &cachingProvider{
            Provider: p,
            cache:    cache,
            ttl:      ttl,
        }
    }
}

type Cache interface {
    Get(key string) (any, bool)
    Set(key string, value any, ttl time.Duration)
    Delete(key string)
}

// Logging middleware
func WithLogging(logger Logger) Middleware {
    return func(p core.Provider) core.Provider {
        return &loggingProvider{
            Provider: p,
            logger:   logger,
        }
    }
}

type Logger interface {
    Info(msg string, fields ...Field)
    Error(msg string, err error, fields ...Field)
}

// Metrics middleware
func WithMetrics(recorder MetricRecorder) Middleware {
    return func(p core.Provider) core.Provider {
        return &metricsProvider{
            Provider: p,
            recorder: recorder,
        }
    }
}

type MetricRecorder interface {
    RecordLatency(provider, model string, latencyMS int64)
    RecordTokens(provider, model string, input, output int)
    RecordCost(provider, model string, costUSD float64)
    RecordError(provider, model string, err error)
}
```

## Provider Adapters

Each provider adapter implements the core `Provider` interface with provider-specific features.

### Common Adapter Pattern

```go
package provider

import (
    "github.com/shillcollin/gai/core"
)

// Adapter implements core.Provider for a specific AI provider
type Adapter struct {
    apiKey     string
    baseURL    string
    model      string
    httpClient *http.Client
    opts       Options
}

// Options configure the adapter
type Options struct {
    APIKey      string
    BaseURL     string
    Model       string
    HTTPClient  *http.Client
    MaxRetries  int
    Timeout     time.Duration
    Headers     map[string]string
    // Provider-specific options
}

// Option functions for configuration
type Option func(*Options)

func WithAPIKey(key string) Option
func WithModel(model string) Option
func WithBaseURL(url string) Option
func WithHTTPClient(client *http.Client) Option
func WithHeaders(headers map[string]string) Option
func WithTimeout(d time.Duration) Option

// New creates a new adapter
func New(opts ...Option) *Adapter {
    options := defaultOptions()
    for _, opt := range opts {
        opt(&options)
    }

    return &Adapter{
        apiKey:     options.APIKey,
        baseURL:    options.BaseURL,
        model:      options.Model,
        httpClient: options.HTTPClient,
        opts:       options,
    }
}

// Provider interface implementation
func (a *Adapter) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error)
func (a *Adapter) StreamText(ctx context.Context, req core.Request) (*core.Stream, error)
func (a *Adapter) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error)
func (a *Adapter) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error)
func (a *Adapter) Capabilities() core.Capabilities
```

### Provider-Specific Options

Each provider exposes advanced features through the `ProviderOptions` field on the `core.Request`. The SDK offers a hybrid approach for setting these options to provide both type safety and flexibility.

#### Recommended: Typed Option Builders

For common parameters, each provider package (e.g., `openai`, `gemini`) offers type-safe functional options. This is the recommended approach as it prevents typos, is discoverable via editor autocompletion, and is validated at compile time.

Each provider package exposes a `BuildProviderOptions` helper to construct the options map.

```go
import "github.com/shillcollin/gai/providers/openai"
import "github.com/shillcollin/gai/providers/anthropic"
import "github.com/shillcollin/gai/providers/gemini"

// OpenAI specific options using typed builders
req := core.Request{
    Messages: msgs,
    ProviderOptions: openai.BuildProviderOptions(
        openai.WithJSONMode(),
        openai.WithSeed(42),
        openai.WithPresencePenalty(0.5),
    ),
}

// Anthropic specific options using typed builders
req := core.Request{
    Messages: msgs,
    ProviderOptions: anthropic.BuildProviderOptions(
        anthropic.WithThinkingEnabled(true),
        anthropic.WithMaxThinkingTokens(8192),
    ),
}

// Gemini specific options using typed builders
req := core.Request{
    Messages: msgs,
    ProviderOptions: gemini.BuildProviderOptions(
        gemini.WithGrounding("web"),
        gemini.WithThoughtSignatures("require"),
    ),
}
```

#### Advanced: Raw Key-Value for New Features

The underlying `ProviderOptions` field is a `map[string]any`. This allows you to set new, experimental, or niche provider parameters immediately, without waiting for the SDK to be updated.

You can combine both approaches for maximum flexibility.

```go
// Hybrid approach: use typed builders for stable options and
// add a raw key for a new, hypothetical experimental feature.

// 1. Start with the typed builders.
opts := openai.BuildProviderOptions(
    openai.WithSeed(42),
)

// 2. Add the experimental key directly to the map.
opts["openai.experimental_new_feature"] = true
opts["openai.another_one"] = 123

// 3. Use the final map in the request.
req := core.Request{
    Messages: msgs,
    ProviderOptions: opts,
}
``````

## Helper Functions

Utility functions for common operations:

```go
// Token estimation
func EstimateTokens(req core.Request) TokenEstimate
func EstimateMessageTokens(msg core.Message) int
func EstimateTextTokens(text string) int

// Type-safe wrappers for structured outputs
func GenerateObjectTyped[T any](ctx context.Context, p core.Provider, req core.Request) (*core.ObjectResult[T], error)
func StreamObjectTyped[T any](ctx context.Context, p core.Provider, req core.Request) (*core.ObjectStream[T], error)

// Message builders
func SystemMessage(text string) core.Message
func UserMessage(parts ...core.Part) core.Message
func AssistantMessage(text string) core.Message

// Part builders
func TextPart(text string) core.Text
func ImagePart(bytes []byte, mime string) core.Image
func ImageURLPart(url string) core.ImageURL
func FilePart(path string) core.File

// Request builders
func SimpleRequest(prompt string) core.Request
func ChatRequest(messages []core.Message) core.Request

// Stream helpers
func CollectStream(stream *core.Stream) (*core.TextResult, error)
func StreamToWriter(stream *core.Stream, w io.Writer) error
// Inspect non-fatal adjustments via result.Warnings / stream.Warnings()

// Error helpers
func WrapProviderError(err error, provider string) error
func IsRetryable(err error) bool
func GetErrorCode(err error) core.ErrorCode

// JSON helpers
func ValidateJSON(data []byte, schema *JSONSchema) error
func RepairJSON(data []byte) ([]byte, error)
func PrettyJSON(data []byte) ([]byte, error)

// Safety helpers
func IsSafe(events []core.SafetyEvent, threshold core.SafetyLevel) bool
func FilterBySafety(text string, events []core.SafetyEvent) string

// Cost calculations
func CalculateCost(usage core.Usage, provider, model string) float64
func GetModelPricing(provider, model string) ModelPricing

type ModelPricing struct {
    InputPer1K  float64
    OutputPer1K float64
    Currency    string
}
```

## Constants and Enums

Common constants used throughout the SDK:

```go
// Common models
const (
    ModelGPT4       = "gpt-4"
    ModelGPT4Turbo  = "gpt-4-turbo"
    ModelGPT4Mini   = "gpt-4o-mini"
    ModelGPT35      = "gpt-3.5-turbo"

    ModelClaude3Opus   = "claude-3-opus"
    ModelClaude3Sonnet = "claude-3-7-sonnet"
    ModelClaude3Haiku  = "claude-3-5-haiku"

    ModelGemini15Pro   = "gemini-1.5-pro"
    ModelGemini15Flash = "gemini-1.5-flash"
    ModelGemini20Flash = "gemini-2.0-flash"
)

// Token limits
const (
    MaxTokensGPT4     = 128000
    MaxTokensClaude   = 200000
    MaxTokensGemini   = 2097152

    DefaultMaxTokens  = 4096
    DefaultMaxSteps   = 10
    DefaultMaxParallel = 5
)

// Timeouts
const (
    DefaultTimeout     = 60 * time.Second
    DefaultToolTimeout = 30 * time.Second
    StreamTimeout      = 5 * time.Minute
)

// Stream event schema version
const SchemaVersion = "gai.events.v1"
```

## Thread Safety

All types in the SDK are designed with concurrency in mind:

- **Provider implementations**: Thread-safe, can be shared across goroutines
- **Stream**: Safe for concurrent reads from Events() channel
- **Runner**: Thread-safe, executes tools concurrently with proper synchronization
- **Registry**: Thread-safe with RWMutex for template access
- **Cache implementations**: Must be thread-safe

## Best Practices

1. **Provider Initialization**: Create providers once and reuse them
2. **Context Usage**: Always pass context for cancellation support
3. **Error Handling**: Check error types using helper functions
4. **Resource Cleanup**: Always defer Close() on streams
5. **Observability**: Initialize observability early in main()
6. **Token Management**: Monitor usage to prevent hitting limits
7. **Rate Limiting**: Implement application-level rate limiting
8. **Retries**: Configure appropriate retry strategies for production

This API reference covers the complete public API surface of the gai SDK. For detailed usage examples, see the [Developer Guide](DEVELOPER_GUIDE.md). For provider-specific features, see [Provider Documentation](PROVIDERS.md).

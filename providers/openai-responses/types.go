package openairesponses

import (
	"encoding/json"
)

// ResponsesRequest represents a request to the OpenAI Responses API.
type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              any               `json:"input"`                  // string | ResponseInputParam | []ResponseInputParam
	Instructions       string            `json:"instructions,omitempty"` // System/developer instructions
	Tools              []ResponseTool    `json:"tools,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"` // For stateful multi-turn
	Stream             bool              `json:"stream,omitempty"`
	Background         bool              `json:"background,omitempty"` // Async mode
	Reasoning          *ReasoningParams  `json:"reasoning,omitempty"`
	Store              *bool             `json:"store,omitempty"` // Default: true
	Metadata           map[string]any    `json:"metadata,omitempty"`
	Text               *TextFormatParams `json:"text,omitempty"` // For structured outputs
	Temperature        float32           `json:"temperature,omitempty"`
	TopP               float32           `json:"top_p,omitempty"`
	MaxOutputTokens    int               `json:"max_output_tokens,omitempty"`
	ToolChoice         string            `json:"tool_choice,omitempty"` // auto | none | required
}

// ActualResponsesResponse represents the actual response structure from the API
type ActualResponsesResponse struct {
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	CreatedAt          float64              `json:"created_at"`
	Status             string               `json:"status"`
	Background         bool                 `json:"background"`
	Billing            *Billing             `json:"billing"`
	Error              *ResponseError       `json:"error"`
	IncompleteDetails  any                  `json:"incomplete_details"`
	Instructions       *string              `json:"instructions"`
	MaxOutputTokens    *int                 `json:"max_output_tokens"`
	MaxToolCalls       *int                 `json:"max_tool_calls"`
	Model              string               `json:"model"`
	Output             []ActualResponseItem `json:"output"`
	ParallelToolCalls  bool                 `json:"parallel_tool_calls"`
	PreviousResponseID *string              `json:"previous_response_id"`
	PromptCacheKey     *string              `json:"prompt_cache_key"`
	Reasoning          *ActualReasoning     `json:"reasoning"`
	SafetyIdentifier   *string              `json:"safety_identifier"`
	ServiceTier        string               `json:"service_tier"`
	Store              bool                 `json:"store"`
	Temperature        float32              `json:"temperature"`
	Text               *ActualTextFormat    `json:"text"`
	ToolChoice         string               `json:"tool_choice"`
	Tools              []json.RawMessage    `json:"tools"`
	TopLogprobs        int                  `json:"top_logprobs"`
	TopP               float32              `json:"top_p"`
	Truncation         string               `json:"truncation"`
	Usage              *ActualUsage         `json:"usage"`
	User               *string              `json:"user"`
	Metadata           map[string]any       `json:"metadata"`
}

// Billing information
type Billing struct {
	Payer string `json:"payer"`
}

// ActualResponseItem represents an actual output item
type ActualResponseItem struct {
	ID      string              `json:"id"`
	Type    string              `json:"type"`
	Status  string              `json:"status,omitempty"`
	Content []ActualContentPart `json:"content,omitempty"`
	Role    string              `json:"role,omitempty"`

	// For tool calls
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ActualContentPart represents actual content structure
type ActualContentPart struct {
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	Annotations []json.RawMessage `json:"annotations,omitempty"`
	Logprobs    []json.RawMessage `json:"logprobs,omitempty"`
}

// ActualReasoning from the response
type ActualReasoning struct {
	Effort  *string `json:"effort"`
	Summary *string `json:"summary"`
}

// ActualTextFormat from the response
type ActualTextFormat struct {
	Format    *ActualFormat `json:"format"`
	Verbosity string        `json:"verbosity,omitempty"`
}

// ActualFormat specification
type ActualFormat struct {
	Type string `json:"type"`
}

// ActualUsage from the response
type ActualUsage struct {
	InputTokens         int                  `json:"input_tokens"`
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokens        int                  `json:"output_tokens"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`
	TotalTokens         int                  `json:"total_tokens"`
}

// InputTokensDetails breakdown
type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// OutputTokensDetails breakdown
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// Additional types needed by the client

// ResponseInputParam represents a message in the input.
type ResponseInputParam struct {
	Role    string        `json:"role"` // user | developer | assistant | tool
	Content []ContentPart `json:"content"`
}

// ContentPart represents different types of content in a message.
type ContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   string          `json:"image_url,omitempty"`
	Detail     string          `json:"detail,omitempty"` // auto | low | high
	InputAudio *InputAudioData `json:"input_audio,omitempty"`
	FileID     string          `json:"file_id,omitempty"`
}

// InputAudioData represents audio input data.
type InputAudioData struct {
	Data   string `json:"data"`   // base64
	Format string `json:"format"` // wav | mp3 | flac | opus | pcm16
}

// ReasoningParams controls reasoning behavior.
type ReasoningParams struct {
	Effort  string  `json:"effort,omitempty"`  // low | medium | high
	Summary *string `json:"summary,omitempty"` // auto | detailed | null
}

// TextFormatParams controls structured output formatting.
type TextFormatParams struct {
	Format    *TextFormat `json:"format,omitempty"`
	Strict    bool        `json:"strict,omitempty"`
	Stop      []string    `json:"stop,omitempty"`
	Verbosity string      `json:"verbosity,omitempty"`
}

// TextFormat specifies the format for structured outputs.
type TextFormat struct {
	Type       string      `json:"type"` // text | json_schema
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema represents a JSON Schema definition.
type JSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

// ResponseTool represents a tool available to the model.
type ResponseTool struct {
	Type string `json:"type"` // web_search | file_search | code_interpreter | image_generation | mcp | function

	// For hosted tools
	VectorStoreIDs  []string   `json:"vector_store_ids,omitempty"` // file_search
	Container       *Container `json:"container,omitempty"`        // code_interpreter
	Model           string     `json:"model,omitempty"`            // image_generation
	ServerLabel     string     `json:"server_label,omitempty"`     // mcp
	ServerURL       string     `json:"server_url,omitempty"`       // mcp
	RequireApproval string     `json:"require_approval,omitempty"` // mcp: never | always | auto

	// For custom function tools
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Container configuration for code_interpreter.
type Container struct {
	Type string `json:"type"` // auto
}

// ResponseError represents an error from the API.
type ResponseError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// StreamEvent types for SSE streaming
type StreamEvent struct {
	Event string          `json:"-"`    // The event type (e.g., "response.created")
	Data  json.RawMessage `json:"data"` // The event data
}

type StreamEventResponseCreated struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

type StreamEventTextDelta struct {
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

type StreamEventTextDone struct {
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

type StreamEventReasoningDelta struct {
	Delta string `json:"delta"`
}

type StreamEventReasoningDone struct {
	Content string `json:"content"`
	Summary string `json:"summary,omitempty"`
}

type StreamEventToolCall struct {
	Index     int            `json:"index"`
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type StreamEventCompleted struct {
	ID    string       `json:"id"`
	Usage *ActualUsage `json:"usage,omitempty"`
}

type StreamEventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

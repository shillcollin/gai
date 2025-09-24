package openairesponses

import (
	"encoding/json"
)

// ResponsesRequest represents a request to the OpenAI Responses API.
type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              any               `json:"input"`
	Instructions       string            `json:"instructions,omitempty"`
	Modalities         []string          `json:"modalities,omitempty"`
	Tools              []ResponseTool    `json:"tools,omitempty"`
	ToolChoice         any               `json:"tool_choice,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Background         bool              `json:"background,omitempty"`
	Reasoning          *ReasoningParams  `json:"reasoning,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	Metadata           map[string]any    `json:"metadata,omitempty"`
	User               string            `json:"user,omitempty"`
	Text               *TextFormatParams `json:"text,omitempty"`
	Audio              *AudioParams      `json:"audio,omitempty"`
	Temperature        float32           `json:"temperature,omitempty"`
	TopP               float32           `json:"top_p,omitempty"`
	TopK               int               `json:"top_k,omitempty"`
	Seed               *int              `json:"seed,omitempty"`
	Stop               []string          `json:"stop,omitempty"`
	MaxPromptTokens    int               `json:"max_prompt_tokens,omitempty"`
	MaxOutputTokens    int               `json:"max_output_tokens,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	ParallelToolLimit  int               `json:"parallel_tool_call_limit,omitempty"`
	MaxToolCalls       int               `json:"max_tool_calls,omitempty"`
	Truncation         string            `json:"truncation,omitempty"`
	Prediction         map[string]any    `json:"prediction,omitempty"`
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
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
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

// AudioParams configures audio output settings when modalities include audio.
type AudioParams struct {
	Voice      string `json:"voice,omitempty"`
	Format     string `json:"format,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Language   string `json:"language,omitempty"`
	Quality    string `json:"quality,omitempty"`
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

// ToolChoiceOption allows forcing a specific tool call strategy.
type ToolChoiceOption struct {
	Type     string              `json:"type"`
	Function *ToolChoiceFunction `json:"function,omitempty"`
}

// ToolChoiceFunction describes a targeted function tool selection.
type ToolChoiceFunction struct {
	Name string `json:"name"`
}

// FunctionCallParam encodes a model-issued function call in the request history.
type FunctionCallParam struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// FunctionCallOutputParam encodes the tool result returned to the model.
type FunctionCallOutputParam struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// StreamEvent types for SSE streaming
type StreamEvent struct {
	Event string          `json:"-"`    // The event type (e.g., "response.created")
	Data  json.RawMessage `json:"data"` // The event data
}

type StreamEventResponseCreated struct {
	Response ActualResponsesResponse `json:"response"`
}

type StreamEventTextDelta struct {
	ItemID       string            `json:"item_id"`
	OutputIndex  int               `json:"output_index"`
	ContentIndex int               `json:"content_index"`
	Delta        string            `json:"delta"`
	Logprobs     []json.RawMessage `json:"logprobs,omitempty"`
	Obfuscation  string            `json:"obfuscation,omitempty"`
}

type StreamEventTextDone struct {
	ItemID       string            `json:"item_id"`
	OutputIndex  int               `json:"output_index"`
	ContentIndex int               `json:"content_index"`
	Text         string            `json:"text"`
	Logprobs     []json.RawMessage `json:"logprobs,omitempty"`
}

type StreamEventReasoningDelta struct {
	ItemID       string `json:"item_id,omitempty"`
	OutputIndex  int    `json:"output_index,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"`
	Delta        string `json:"delta"`
}

type StreamEventReasoningDone struct {
	ItemID       string `json:"item_id,omitempty"`
	OutputIndex  int    `json:"output_index,omitempty"`
	ContentIndex int    `json:"content_index,omitempty"`
	Content      string `json:"content"`
	Summary      string `json:"summary,omitempty"`
}

type StreamEventToolCall struct {
	Index     int            `json:"index"`
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type StreamEventCompleted struct {
	Response ActualResponsesResponse `json:"response"`
}

type StreamEventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type StreamEventOutputItemAdded struct {
	Item struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Status    string `json:"status,omitempty"`
		Arguments string `json:"arguments,omitempty"`
		CallID    string `json:"call_id,omitempty"`
		Name      string `json:"name,omitempty"`
		Summary   any    `json:"summary,omitempty"`
	} `json:"item"`
}

type StreamEventFunctionCallArgumentsDelta struct {
	ItemID string `json:"item_id"`
	Delta  string `json:"delta"`
}

type StreamEventFunctionCallArgumentsDone struct {
	ItemID    string `json:"item_id"`
	Arguments string `json:"arguments"`
}

type StreamEventContentPart struct {
	ItemID       string              `json:"item_id"`
	OutputIndex  int                 `json:"output_index"`
	ContentIndex int                 `json:"content_index"`
	Part         ResponseContentPart `json:"part"`
}

type ResponseContentPart struct {
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	Annotations []json.RawMessage `json:"annotations,omitempty"`
	Logprobs    []json.RawMessage `json:"logprobs,omitempty"`
}

type StreamEventOutputAudioDelta struct {
	ItemID       string            `json:"item_id"`
	OutputIndex  int               `json:"output_index"`
	ContentIndex int               `json:"content_index"`
	Delta        AudioDeltaPayload `json:"delta"`
}

type AudioDeltaPayload struct {
	Audio AudioChunk `json:"audio"`
}

type AudioChunk struct {
	Data   string `json:"data"`
	Format string `json:"format,omitempty"`
}

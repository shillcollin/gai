package openai

import (
	"encoding/json"
	"strings"

	"github.com/shillcollin/gai/core"
)

type chatCompletionRequest struct {
	Model               string             `json:"model"`
	Messages            []openAIMessage    `json:"messages"`
	Temperature         float32            `json:"temperature,omitempty"`
	MaxTokens           int                `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                `json:"max_completion_tokens,omitempty"`
	TopP                float32            `json:"top_p,omitempty"`
	TopK                int                `json:"top_k,omitempty"`
	Stream              bool               `json:"stream,omitempty"`
	StreamOptions       *streamOptions     `json:"stream_options,omitempty"`
	Tools               []openAITool       `json:"tools,omitempty"`
	ToolChoice          any                `json:"tool_choice,omitempty"`
	PresencePenalty     float32            `json:"presence_penalty,omitempty"`
	FrequencyPenalty    float32            `json:"frequency_penalty,omitempty"`
	LogitBias           map[string]float32 `json:"logit_bias,omitempty"`
	User                string             `json:"user,omitempty"`
	ResponseFormat      any                `json:"response_format,omitempty"`
	Seed                *int               `json:"seed,omitempty"`
	Stop                []string           `json:"stop,omitempty"`
	SafetySettings      map[string]any     `json:"safety_settings,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    []openAIContent  `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIContent struct {
	Type       string            `json:"type"`
	Text       string            `json:"text,omitempty"`
	ImageURL   *openAIImageURL   `json:"image_url,omitempty"`
	Image      *openAIImageInput `json:"image,omitempty"`
	InputAudio *openAIInputAudio `json:"input_audio,omitempty"`
	InputVideo *openAIInputVideo `json:"input_video,omitempty"`
}

type openAIImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type openAIImageInput struct {
	B64JSON  string `json:"b64_json"`
	MimeType string `json:"mime_type,omitempty"`
}

type openAIInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type openAIInputVideo struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   openAIUsage            `json:"usage"`
}

type chatCompletionChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type streamDelta struct {
	ID      string              `json:"id"`
	Model   string              `json:"model"`
	Seq     int                 `json:"created"`
	Choices []streamDeltaChoice `json:"choices"`
	Usage   *openAIUsage        `json:"usage,omitempty"`
}

type streamDeltaChoice struct {
	Delta        openAIMessage `json:"delta"`
	FinishReason string        `json:"finish_reason"`
}

func (u openAIUsage) toCore() core.Usage {
	return core.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
}

func (m openAIMessage) JoinText() string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func (m openAIMessage) MarshalJSON() ([]byte, error) {
	type alias openAIMessage
	return json.Marshal(alias(m))
}

func (m *openAIMessage) UnmarshalJSON(data []byte) error {
	type rawMessage struct {
		Role       string           `json:"role"`
		Content    json.RawMessage  `json:"content"`
		ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
		ToolCallID string           `json:"tool_call_id,omitempty"`
		Name       string           `json:"name,omitempty"`
	}
	if len(data) == 0 {
		return nil
	}
	var raw rawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	m.ToolCalls = raw.ToolCalls
	m.ToolCallID = raw.ToolCallID
	m.Name = raw.Name
	m.Content = nil
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		return nil
	}
	switch raw.Content[0] {
	case '{', '[':
		var parts []openAIContent
		if err := json.Unmarshal(raw.Content, &parts); err != nil {
			return err
		}
		m.Content = parts
		return nil
	default:
		var text string
		if err := json.Unmarshal(raw.Content, &text); err != nil {
			return err
		}
		if text != "" {
			m.Content = []openAIContent{{Type: "text", Text: text}}
		}
		return nil
	}
}

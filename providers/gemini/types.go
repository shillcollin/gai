package gemini

import (
	"strings"

	"github.com/shillcollin/gai/core"
)

type geminiRequest struct {
	Model            string                 `json:"model"`
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
	SafetySettings   []geminiSafetySetting  `json:"safetySettings,omitempty"`
	Tools            []geminiTool           `json:"tools,omitempty"`
	ToolConfig       *geminiToolConfig      `json:"toolConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature      float32               `json:"temperature,omitempty"`
	MaxOutputTokens  int                   `json:"maxOutputTokens,omitempty"`
	TopP             float32               `json:"topP,omitempty"`
	ThinkingConfig   *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseMimeType string                `json:"responseMimeType,omitempty"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *geminiInlineData       `json:"inlineData,omitempty"`
	FileData         *geminiFileData         `json:"fileData,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data"`
}

type geminiFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
	ID   string         `json:"id,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
	ID       string         `json:"id,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type geminiFunctionCallingConfig struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiStreamResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

type geminiThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts,omitempty"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

func (r geminiResponse) JoinText() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return r.Candidates[0].Content.JoinText()
}

func (r geminiStreamResponse) JoinText() string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return r.Candidates[0].Content.JoinText()
}

func (c geminiContent) JoinText() string {
	var b strings.Builder
	for _, part := range c.Parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

func (u geminiUsageMetadata) toCore() core.Usage {
	return core.Usage{
		InputTokens:     u.PromptTokenCount,
		OutputTokens:    u.CandidatesTokenCount,
		ReasoningTokens: u.ThoughtsTokenCount,
		TotalTokens:     u.TotalTokenCount,
	}
}

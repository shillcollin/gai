package gemini

import "strings"

type geminiRequest struct {
	Model            string                 `json:"model"`
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
	SafetySettings   []geminiSafetySetting  `json:"safetySettings,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     float32 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	TopP            float32 `json:"topP,omitempty"`
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
	Text string `json:"text,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiStreamResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
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

package core

import (
	"strings"

	"github.com/shillcollin/gai/internal/tokens"
)

// TokenEstimate summarises estimated token usage.
type TokenEstimate struct {
	Input     int
	MaxOutput int
	Total     int
}

// EstimateTokens estimates tokens for a request using heuristics.
func EstimateTokens(req Request) TokenEstimate {
	input := 0
	for _, msg := range req.Messages {
		input += EstimateMessageTokens(msg)
	}
	maxOut := req.MaxTokens
	if maxOut == 0 {
		maxOut = tokens.DefaultMaxOutput
	}
	return TokenEstimate{Input: input, MaxOutput: maxOut, Total: input + maxOut}
}

// EstimateMessageTokens estimates tokens for a message.
func EstimateMessageTokens(msg Message) int {
	total := tokens.EstimateText(string(msg.Role))
	for _, part := range msg.Parts {
		total += estimatePartTokens(part)
	}
	return total
}

// EstimateTextTokens estimates tokens from raw text.
func EstimateTextTokens(text string) int {
	return tokens.EstimateText(text)
}

func estimatePartTokens(part Part) int {
	switch p := part.(type) {
	case Text:
		return tokens.EstimateText(p.Text)
	case Image:
		if err := p.Source.Validate(); err == nil && p.Source.Size > 0 {
			return tokens.EstimateBytes(p.Source.Size)
		}
		return 512
	case ImageURL:
		return tokens.EstimateText(p.URL)
	case Audio, Video:
		return 1024
	case File:
		if p.Source.Size > 0 {
			return tokens.EstimateBytes(p.Source.Size)
		}
		return 256
	case ToolCall:
		keys := make([]string, 0, len(p.Input))
		for k := range p.Input {
			keys = append(keys, k)
		}
		return tokens.EstimateText(strings.Join(keys, " ")) + tokens.EstimateJSON(p.Input)
	case ToolResult:
		if p.Error != "" {
			return tokens.EstimateText(p.Error)
		}
		return tokens.EstimateJSON(p.Result)
	default:
		return 32
	}
}

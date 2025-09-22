package obs

import (
	"fmt"

	"github.com/shillcollin/gai/core"
)

// UsageFromCore builds a UsageTokens struct from a core.Usage value.
func UsageFromCore(u core.Usage) UsageTokens {
	return UsageTokens{
		InputTokens:     u.InputTokens,
		OutputTokens:    u.OutputTokens,
		TotalTokens:     u.TotalTokens,
		ReasoningTokens: u.ReasoningTokens,
		CachedTokens:    u.CachedInputTokens,
		AudioTokens:     u.AudioTokens,
		CostUSD:         u.CostUSD,
	}
}

// MessageFromCore converts a core.Message into an observability-safe message.
func MessageFromCore(msg core.Message) Message {
	text := ""
	data := map[string]any{}
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case core.Text:
			if text != "" {
				text += "\n"
			}
			text += p.Text
		case core.ToolCall:
			dataKey := fmt.Sprintf("tool_call_%s", p.ID)
			data[dataKey] = map[string]any{
				"id":    p.ID,
				"name":  p.Name,
				"input": p.Input,
			}
		case core.ToolResult:
			dataKey := fmt.Sprintf("tool_result_%s", p.ID)
			data[dataKey] = map[string]any{
				"id":     p.ID,
				"name":   p.Name,
				"result": p.Result,
				"error":  p.Error,
			}
		default:
			pt := string(part.Type())
			if pt == "" {
				pt = fmt.Sprintf("part_%T", part)
			}
			data[pt] = part.Content()
		}
	}
	if len(data) == 0 {
		data = nil
	}
	return Message{Role: string(msg.Role), Text: text, Data: data}
}

// MessagesFromCore converts a slice of core.Message.
func MessagesFromCore(messages []core.Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, MessageFromCore(msg))
	}
	return out
}

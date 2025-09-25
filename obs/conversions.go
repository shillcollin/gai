package obs

import (
	"encoding/json"
	"fmt"
	"sort"

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

// ToolCallsFromSteps flattens tool executions recorded on runner steps into
// observability ToolCallRecord entries.
func ToolCallsFromSteps(steps []core.Step) []ToolCallRecord {
	records := make([]ToolCallRecord, 0)
	for _, step := range steps {
		for _, exec := range step.ToolCalls {
			record := ToolCallRecord{
				Step:       step.Number,
				ID:         exec.Call.ID,
				Name:       exec.Call.Name,
				Input:      NormalizeMap(exec.Call.Input),
				DurationMS: exec.DurationMS,
				Retries:    exec.Retries,
			}
			if exec.Result != nil {
				record.Result = NormalizeValue(exec.Result)
			}
			if exec.Error != nil {
				record.Error = exec.Error.Error()
			}
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Step == records[j].Step {
			return records[i].ID < records[j].ID
		}
		return records[i].Step < records[j].Step
	})
	return records
}

// ToolCallsToAny converts tool call records to JSON-friendly objects.
func ToolCallsToAny(records []ToolCallRecord) []map[string]any {
	if len(records) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		entry := map[string]any{
			"step": rec.Step,
		}
		if rec.ID != "" {
			entry["id"] = rec.ID
		}
		if rec.Name != "" {
			entry["name"] = rec.Name
		}
		if len(rec.Input) > 0 {
			entry["input"] = rec.Input
		}
		if rec.Result != nil {
			entry["result"] = rec.Result
		}
		if rec.Error != "" {
			entry["error"] = rec.Error
		}
		if rec.DurationMS > 0 {
			entry["duration_ms"] = rec.DurationMS
		}
		if rec.Retries > 0 {
			entry["retries"] = rec.Retries
		}
		out = append(out, entry)
	}
	return out
}

// NormalizeMap ensures nested map values are JSON-serializable.
func NormalizeMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	clean := make(map[string]any, len(input))
	for k, v := range input {
		clean[k] = NormalizeValue(v)
	}
	return clean
}

// NormalizeValue flattens arbitrary values into JSON-safe types.
func NormalizeValue(v any) any {
	switch typed := v.(type) {
	case nil,
		string,
		bool,
		int,
		int8,
		int16,
		int32,
		int64,
		uint,
		uint8,
		uint16,
		uint32,
		uint64,
		float32,
		float64:
		return typed
	case map[string]any:
		return NormalizeMap(typed)
	case []any:
		if len(typed) == 0 {
			return []any{}
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, NormalizeValue(item))
		}
		return out
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return string(data)
		}
		return generic
	}
}

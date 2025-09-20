package tokens

import (
	"encoding/json"
	"math"
)

const (
	DefaultMaxOutput = 256
)

// EstimateText approximates tokens for a given text using a 4 characters per token heuristic.
func EstimateText(text string) int {
	if text == "" {
		return 0
	}
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	return int(math.Ceil(float64(runes) / 4.0))
}

// EstimateBytes approximates tokens for a binary payload.
func EstimateBytes(size int64) int {
	if size <= 0 {
		return 0
	}
	return int(math.Ceil(float64(size) / 1024.0))
}

// EstimateJSON approximates tokens for structured JSON by serializing to bytes.
func EstimateJSON(value any) int {
	if value == nil {
		return 0
	}
	data, err := json.Marshal(value)
	if err != nil {
		return EstimateText(err.Error())
	}
	return EstimateText(string(data))
}

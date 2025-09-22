package openairesponses

import "strings"

type modelPolicy struct {
	UseMaxOutputTokens     bool
	AllowTemperature       bool
	AllowTopP              bool
	AllowVerbosity         bool
	AllowReasoning         bool
	AllowedReasoningEffort map[string]struct{}
	VerbosityOptions       map[string]struct{}
}

func defaultPolicy() modelPolicy {
	return modelPolicy{
		UseMaxOutputTokens: true,
		AllowTemperature:   true,
		AllowTopP:          true,
		AllowVerbosity:     true,
		AllowReasoning:     true,
		AllowedReasoningEffort: map[string]struct{}{
			"low":    {},
			"medium": {},
			"high":   {},
		},
		VerbosityOptions: map[string]struct{}{
			"low":    {},
			"medium": {},
			"high":   {},
		},
	}
}

var (
	gpt5Policy = modelPolicy{
		UseMaxOutputTokens: true,
		AllowTemperature:   false,
		AllowTopP:          false,
		AllowVerbosity:     true,
		AllowReasoning:     true,
		AllowedReasoningEffort: map[string]struct{}{
			"minimal": {},
			"low":     {},
			"medium":  {},
			"high":    {},
		},
		VerbosityOptions: map[string]struct{}{
			"low":    {},
			"medium": {},
			"high":   {},
		},
	}

	oSeriesPolicy = modelPolicy{
		UseMaxOutputTokens: true,
		AllowTemperature:   false,
		AllowTopP:          false,
		AllowVerbosity:     false,
		AllowReasoning:     true,
		AllowedReasoningEffort: map[string]struct{}{
			"low":    {},
			"medium": {},
			"high":   {},
		},
		VerbosityOptions: nil,
	}

	gpt41Policy = modelPolicy{
		UseMaxOutputTokens: true,
		AllowTemperature:   true,
		AllowTopP:          true,
		AllowVerbosity:     false,
		AllowReasoning:     true,
		AllowedReasoningEffort: map[string]struct{}{
			"low":    {},
			"medium": {},
			"high":   {},
		},
		VerbosityOptions: nil,
	}
)

func policyForModel(model string) modelPolicy {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-5"):
		return gpt5Policy
	case strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4"):
		return oSeriesPolicy
	case strings.HasPrefix(m, "gpt-4.1"):
		return gpt41Policy
	default:
		return defaultPolicy()
	}
}

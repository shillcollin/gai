package openai

import "strings"

type modelProfile struct {
	UseMaxCompletionTokens bool
	AllowTemperature       bool
	AllowTopP              bool
	AllowTopK              bool
	AllowLogitBias         bool
	AllowPenalties         bool
	AllowSeed              bool
	AllowedReasoningEffort map[string]struct{}
}

func defaultProfile() modelProfile {
	return modelProfile{
		AllowTemperature: true,
		AllowTopP:        true,
		AllowTopK:        true,
		AllowLogitBias:   true,
		AllowPenalties:   true,
		AllowSeed:        true,
	}
}

var (
	gpt5ReasoningEffort = map[string]struct{}{
		"minimal": {},
		"low":     {},
		"medium":  {},
		"high":    {},
	}
	openAIReasoningEffort = map[string]struct{}{
		"low":    {},
		"medium": {},
		"high":   {},
	}
)

var (
	gpt5Profile = modelProfile{
		UseMaxCompletionTokens: true,
		AllowTemperature:       false,
		AllowTopP:              false,
		AllowTopK:              false,
		AllowLogitBias:         false,
		AllowPenalties:         false,
		AllowSeed:              false,
		AllowedReasoningEffort: gpt5ReasoningEffort,
	}

	gpt41Profile = modelProfile{
		UseMaxCompletionTokens: true,
		AllowTemperature:       true,
		AllowTopP:              true,
		AllowTopK:              true,
		AllowLogitBias:         true,
		AllowPenalties:         true,
		AllowSeed:              true,
	}

	oSeriesProfile = modelProfile{
		UseMaxCompletionTokens: true,
		AllowTemperature:       false,
		AllowTopP:              false,
		AllowTopK:              false,
		AllowLogitBias:         false,
		AllowPenalties:         false,
		AllowSeed:              false,
		AllowedReasoningEffort: openAIReasoningEffort,
	}
)

func profileForModel(model string) modelProfile {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-5"):
		return gpt5Profile
	case strings.HasPrefix(m, "gpt-4.1"):
		return gpt41Profile
	case strings.HasPrefix(m, "o4"):
		return oSeriesProfile
	case strings.HasPrefix(m, "o3"):
		return oSeriesProfile
	default:
		return defaultProfile()
	}
}

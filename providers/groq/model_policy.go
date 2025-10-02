package groq


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

// All Groq models currently use the same profile.
func profileForModel(model string) modelProfile {
	return defaultProfile()
}

package gemini

// ProviderOption represents a provider-specific option for Gemini requests.
type ProviderOption func(map[string]any)

// BuildProviderOptions constructs a provider options map suitable for core.Request.
func BuildProviderOptions(opts ...ProviderOption) map[string]any {
	out := make(map[string]any)
	for _, opt := range opts {
		if opt != nil {
			opt(out)
		}
	}
	return out
}

// WithThinkingBudget sets the thinking budget (token count) for Gemini reasoning.
func WithThinkingBudget(tokens int) ProviderOption {
	return func(m map[string]any) {
		m[providerOptionThinkingBudget] = tokens
	}
}

// WithIncludeThoughts toggles inclusion of thought summaries in model responses.
func WithIncludeThoughts(include bool) ProviderOption {
	return func(m map[string]any) {
		m[providerOptionIncludeThought] = include
	}
}

// WithThinkingSummaries is an alias for WithIncludeThoughts.
func WithThinkingSummaries(include bool) ProviderOption {
	return WithIncludeThoughts(include)
}

// WithThoughtSignatures sets the desired thought signature behavior. Accepted values
// include "auto", "require", or "omit". The current API defaults to auto behavior;
// this option is stored for forward compatibility.
func WithThoughtSignatures(mode string) ProviderOption {
	return func(m map[string]any) {
		m["gemini.thought_signatures.mode"] = mode
	}
}

// WithGrounding records the grounding mode (for example, "web").
func WithGrounding(source string) ProviderOption {
	return func(m map[string]any) {
		m["gemini.grounding"] = source
	}
}

// WithResponseMIME sets the desired response MIME type (e.g. application/json).
func WithResponseMIME(mime string) ProviderOption {
	return func(m map[string]any) {
		m[providerOptionResponseMIME] = mime
	}
}

// WithJSONResponse configures the request to return strict JSON payloads.
func WithJSONResponse() ProviderOption {
	return WithResponseMIME("application/json")
}

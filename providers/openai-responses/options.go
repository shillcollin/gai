package openairesponses

import (
	"net/http"
	"time"
)

// options holds configuration for the OpenAI Responses API client.
type options struct {
	apiKey      string
	baseURL     string
	model       string
	httpClient  *http.Client
	timeout     time.Duration
	maxRetries  int
	headers     map[string]string

	// Responses API specific defaults
	store               *bool
	defaultInstructions string
	defaultReasoning    *ReasoningParams
}

// defaultOptions returns the default configuration.
func defaultOptions() options {
	storeTrue := true
	return options{
		baseURL:    "https://api.openai.com/v1",
		model:      "gpt-4o-mini",
		timeout:    120 * time.Second,
		maxRetries: 3,
		headers:    make(map[string]string),
		store:      &storeTrue,
	}
}

// Option is a function that configures the client.
type Option func(*options)

// WithAPIKey sets the OpenAI API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.apiKey = key
	}
}

// WithBaseURL sets a custom base URL (e.g., for proxies or Azure).
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.baseURL = url
	}
}

// WithModel sets the default model to use.
func WithModel(model string) Option {
	return func(o *options) {
		o.model = model
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.httpClient = client
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(n int) Option {
	return func(o *options) {
		o.maxRetries = n
	}
}

// WithHeaders sets additional HTTP headers.
func WithHeaders(headers map[string]string) Option {
	return func(o *options) {
		if o.headers == nil {
			o.headers = make(map[string]string)
		}
		for k, v := range headers {
			o.headers[k] = v
		}
	}
}

// WithStore sets whether to store responses on the server (for stateful multi-turn).
func WithStore(store bool) Option {
	return func(o *options) {
		o.store = &store
	}
}

// WithDefaultInstructions sets default system instructions.
func WithDefaultInstructions(instructions string) Option {
	return func(o *options) {
		o.defaultInstructions = instructions
	}
}

// WithDefaultReasoning sets default reasoning parameters.
func WithDefaultReasoning(reasoning *ReasoningParams) Option {
	return func(o *options) {
		o.defaultReasoning = reasoning
	}
}

// BuildProviderOptions creates provider-specific options for the core.Request.
// These are helper functions for type-safe configuration.
func BuildProviderOptions(opts ...ResponsesProviderOption) map[string]any {
	options := make(map[string]any)
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ResponsesProviderOption is a function that adds provider-specific options.
type ResponsesProviderOption func(map[string]any)

// WithPreviousResponseID sets the previous response ID for stateful conversations.
func WithPreviousResponseID(id string) ResponsesProviderOption {
	return func(m map[string]any) {
		m["openai-responses.previous_response_id"] = id
	}
}

// WithBackground enables background (async) processing.
func WithBackground(background bool) ResponsesProviderOption {
	return func(m map[string]any) {
		m["openai-responses.background"] = background
	}
}

// WithReasoningEffort sets the reasoning effort level.
func WithReasoningEffort(effort string) ResponsesProviderOption {
	return func(m map[string]any) {
		if _, ok := m["openai-responses.reasoning"]; !ok {
			m["openai-responses.reasoning"] = make(map[string]any)
		}
		reasoning := m["openai-responses.reasoning"].(map[string]any)
		reasoning["effort"] = effort
	}
}

// WithReasoningSummary requests reasoning summaries.
func WithReasoningSummary(summary string) ResponsesProviderOption {
	return func(m map[string]any) {
		if _, ok := m["openai-responses.reasoning"]; !ok {
			m["openai-responses.reasoning"] = make(map[string]any)
		}
		reasoning := m["openai-responses.reasoning"].(map[string]any)
		reasoning["summary"] = summary
	}
}

// WithHostedTools adds hosted tools to the request.
func WithHostedTools(tools ...ResponseTool) ResponsesProviderOption {
	return func(m map[string]any) {
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithWebSearch enables web search tool.
func WithWebSearch() ResponsesProviderOption {
	return func(m map[string]any) {
		tools := []ResponseTool{{Type: "web_search"}}
		if existing, ok := m["openai-responses.hosted_tools"].([]ResponseTool); ok {
			tools = append(existing, tools...)
		}
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithFileSearch enables file search with specified vector store IDs.
func WithFileSearch(vectorStoreIDs ...string) ResponsesProviderOption {
	return func(m map[string]any) {
		tool := ResponseTool{
			Type:           "file_search",
			VectorStoreIDs: vectorStoreIDs,
		}
		tools := []ResponseTool{tool}
		if existing, ok := m["openai-responses.hosted_tools"].([]ResponseTool); ok {
			tools = append(existing, tools...)
		}
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithCodeInterpreter enables code interpreter.
func WithCodeInterpreter() ResponsesProviderOption {
	return func(m map[string]any) {
		tool := ResponseTool{
			Type:      "code_interpreter",
			Container: &Container{Type: "auto"},
		}
		tools := []ResponseTool{tool}
		if existing, ok := m["openai-responses.hosted_tools"].([]ResponseTool); ok {
			tools = append(existing, tools...)
		}
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithImageGeneration enables image generation.
func WithImageGeneration(model string) ResponsesProviderOption {
	return func(m map[string]any) {
		if model == "" {
			model = "gpt-image-1"
		}
		tool := ResponseTool{
			Type:  "image_generation",
			Model: model,
		}
		tools := []ResponseTool{tool}
		if existing, ok := m["openai-responses.hosted_tools"].([]ResponseTool); ok {
			tools = append(existing, tools...)
		}
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithMCPServer adds an MCP server tool.
func WithMCPServer(label, url string, requireApproval string) ResponsesProviderOption {
	return func(m map[string]any) {
		tool := ResponseTool{
			Type:            "mcp",
			ServerLabel:     label,
			ServerURL:       url,
			RequireApproval: requireApproval,
		}
		tools := []ResponseTool{tool}
		if existing, ok := m["openai-responses.hosted_tools"].([]ResponseTool); ok {
			tools = append(existing, tools...)
		}
		m["openai-responses.hosted_tools"] = tools
	}
}

// WithStoreResponse controls whether to store the response on the server.
func WithStoreResponse(store bool) ResponsesProviderOption {
	return func(m map[string]any) {
		m["openai-responses.store"] = store
	}
}

// WithMetadata adds metadata to the request.
func WithMetadata(metadata map[string]any) ResponsesProviderOption {
	return func(m map[string]any) {
		m["openai-responses.metadata"] = metadata
	}
}
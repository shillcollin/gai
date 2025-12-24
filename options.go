package gai

import (
	"net/http"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
)

// WithProvider manually registers a provider instance.
// Use this for custom provider configurations or testing.
func WithProvider(id string, provider core.Provider) ClientOption {
	return func(c *Client) {
		c.providers[id] = provider
	}
}

// WithAPIKey configures an API key for a specific provider.
// The provider must be registered (imported) before this option takes effect.
func WithAPIKey(providerID, apiKey string) ClientOption {
	return func(c *Client) {
		factory, ok := GetProviderFactory(providerID)
		if !ok {
			return // Provider not registered, skip silently
		}

		config := factory.DefaultConfig()
		config.APIKey = apiKey
		config.HTTPClient = c.httpClient

		if provider, err := factory.New(config); err == nil {
			c.providers[providerID] = provider
		}
	}
}

// WithBaseURL configures a custom base URL for a specific provider.
// Useful for self-hosted models or proxy endpoints.
func WithBaseURL(providerID, baseURL string) ClientOption {
	return func(c *Client) {
		factory, ok := GetProviderFactory(providerID)
		if !ok {
			return // Provider not registered, skip silently
		}

		config := factory.DefaultConfig()
		config.BaseURL = baseURL
		config.HTTPClient = c.httpClient

		if provider, err := factory.New(config); err == nil {
			c.providers[providerID] = provider
		}
	}
}

// WithProviderConfig configures a provider with full configuration.
func WithProviderConfig(providerID string, config ProviderConfig) ClientOption {
	return func(c *Client) {
		factory, ok := GetProviderFactory(providerID)
		if !ok {
			return // Provider not registered, skip silently
		}

		if config.HTTPClient == nil {
			config.HTTPClient = c.httpClient
		}

		if provider, err := factory.New(config); err == nil {
			c.providers[providerID] = provider
		}
	}
}

// WithDefaultModel sets the default model used when no model is specified.
func WithDefaultModel(model string) ClientOption {
	return func(c *Client) {
		c.defaults.Model = model
	}
}

// WithDefaultTemperature sets the default temperature for all requests.
func WithDefaultTemperature(temp float64) ClientOption {
	return func(c *Client) {
		c.defaults.Temperature = &temp
	}
}

// WithDefaultMaxTokens sets the default max tokens for all requests.
func WithDefaultMaxTokens(n int) ClientOption {
	return func(c *Client) {
		c.defaults.MaxTokens = &n
	}
}

// WithAlias defines a model alias.
// Aliases allow using short names for model strings:
//
//	client := gai.NewClient(
//	    gai.WithAlias("fast", "groq/llama3-70b-8192"),
//	    gai.WithAlias("smart", "anthropic/claude-3-5-sonnet"),
//	)
//	text, _ := client.Text(ctx, gai.Request("fast").User("Hello"))
func WithAlias(alias, model string) ClientOption {
	return func(c *Client) {
		c.aliases[alias] = model
	}
}

// WithAliases sets multiple aliases at once.
func WithAliases(aliases map[string]string) ClientOption {
	return func(c *Client) {
		for alias, model := range aliases {
			c.aliases[alias] = model
		}
	}
}

// WithHTTPClient sets a custom HTTP client for all providers.
// Must be called before providers are configured.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithToolTimeout sets the timeout for individual tool executions (for Run).
func WithToolTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithToolTimeout(d))
	}
}

// WithMaxParallel sets the maximum number of parallel tool executions (for Run).
func WithMaxParallel(n int) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithMaxParallel(n))
	}
}

// WithToolRetry configures retry behavior for tool executions (for Run).
func WithToolRetry(retry runner.ToolRetry) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithToolRetry(retry))
	}
}

// WithOnToolError configures error handling for tool executions (for Run).
func WithOnToolError(mode runner.ToolErrorMode) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithOnToolError(mode))
	}
}

// WithMemo configures memoization for tool results (for Run).
func WithMemo(memo runner.ToolMemo) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithMemo(memo))
	}
}

// WithInterceptor adds a runner interceptor for lifecycle hooks (for Run).
func WithInterceptor(interceptor runner.Interceptor) ClientOption {
	return func(c *Client) {
		c.runnerOpts = append(c.runnerOpts, runner.WithInterceptor(interceptor))
	}
}

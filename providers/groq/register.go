package groq

import (
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"
)

func init() {
	gai.RegisterProvider("groq", &Factory{})
}

// Factory creates Groq provider instances.
type Factory struct{}

// New creates a new Groq provider with the given configuration.
func (f *Factory) New(config gai.ProviderConfig) (core.Provider, error) {
	var opts []Option

	if config.APIKey != "" {
		opts = append(opts, WithAPIKey(config.APIKey))
	}
	if config.BaseURL != "" {
		opts = append(opts, WithBaseURL(config.BaseURL))
	}
	if config.DefaultModel != "" {
		opts = append(opts, WithModel(config.DefaultModel))
	}
	if config.HTTPClient != nil {
		opts = append(opts, WithHTTPClient(config.HTTPClient))
	}
	for k, v := range config.Headers {
		opts = append(opts, WithHeader(k, v))
	}

	return New(opts...), nil
}

// DefaultConfig returns default configuration from environment variables.
func (f *Factory) DefaultConfig() gai.ProviderConfig {
	return gai.ProviderConfig{
		APIKey:  os.Getenv("GROQ_API_KEY"),
		BaseURL: os.Getenv("GROQ_BASE_URL"),
	}
}

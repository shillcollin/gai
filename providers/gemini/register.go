package gemini

import (
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/core"
)

func init() {
	gai.RegisterProvider("gemini", &Factory{})
}

// Factory creates Gemini provider instances.
type Factory struct{}

// New creates a new Gemini provider with the given configuration.
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

	return New(opts...), nil
}

// DefaultConfig returns default configuration from environment variables.
func (f *Factory) DefaultConfig() gai.ProviderConfig {
	return gai.ProviderConfig{
		APIKey:  os.Getenv("GOOGLE_API_KEY"),
		BaseURL: os.Getenv("GEMINI_BASE_URL"),
	}
}

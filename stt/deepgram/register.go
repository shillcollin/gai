package deepgram

import (
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/stt"
)

func init() {
	gai.RegisterSTTProvider("deepgram", &Factory{})
}

// Factory creates Deepgram STT provider instances.
type Factory struct{}

// New implements gai.STTProviderFactory.
func (f *Factory) New(config gai.STTProviderConfig) (stt.Provider, error) {
	var opts []Option

	if config.APIKey != "" {
		opts = append(opts, WithAPIKey(config.APIKey))
	}
	if config.BaseURL != "" {
		opts = append(opts, WithBaseURL(config.BaseURL))
	}
	if config.HTTPClient != nil {
		opts = append(opts, WithHTTPClient(config.HTTPClient))
	}

	// Check for model in custom options
	if model, ok := config.Custom["model"].(string); ok && model != "" {
		opts = append(opts, WithModel(model))
	}

	return New(opts...), nil
}

// DefaultConfig implements gai.STTProviderFactory.
func (f *Factory) DefaultConfig() gai.STTProviderConfig {
	return gai.STTProviderConfig{
		APIKey:  os.Getenv("DEEPGRAM_API_KEY"),
		BaseURL: os.Getenv("DEEPGRAM_BASE_URL"),
	}
}

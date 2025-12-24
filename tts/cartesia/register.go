package cartesia

import (
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/tts"
)

func init() {
	gai.RegisterTTSProvider("cartesia", &Factory{})
}

// Factory creates Cartesia TTS provider instances.
type Factory struct{}

// New implements gai.TTSProviderFactory.
func (f *Factory) New(config gai.TTSProviderConfig) (tts.Provider, error) {
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

	// Check for default voice in custom options
	if voice, ok := config.Custom["voice"].(string); ok && voice != "" {
		opts = append(opts, WithVoice(voice))
	}

	// Check for API version in custom options
	if version, ok := config.Custom["api_version"].(string); ok && version != "" {
		opts = append(opts, WithAPIVersion(version))
	}

	return New(opts...), nil
}

// DefaultConfig implements gai.TTSProviderFactory.
func (f *Factory) DefaultConfig() gai.TTSProviderConfig {
	return gai.TTSProviderConfig{
		APIKey:  os.Getenv("CARTESIA_API_KEY"),
		BaseURL: os.Getenv("CARTESIA_BASE_URL"),
	}
}

package elevenlabs

import (
	"os"

	"github.com/shillcollin/gai"
	"github.com/shillcollin/gai/tts"
)

func init() {
	gai.RegisterTTSProvider("elevenlabs", &Factory{})
}

// Factory creates ElevenLabs TTS provider instances.
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

	return New(opts...), nil
}

// DefaultConfig implements gai.TTSProviderFactory.
func (f *Factory) DefaultConfig() gai.TTSProviderConfig {
	return gai.TTSProviderConfig{
		APIKey:  os.Getenv("ELEVENLABS_API_KEY"),
		BaseURL: os.Getenv("ELEVENLABS_BASE_URL"),
	}
}

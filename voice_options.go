package gai

import (
	"github.com/shillcollin/gai/stt"
	"github.com/shillcollin/gai/tts"
)

// VoiceConfig combines STT, LLM, and TTS configuration for voice pipelines.
type VoiceConfig struct {
	STT string // STT provider/model (e.g., "deepgram/nova-2")
	LLM string // LLM provider/model (e.g., "anthropic/claude-3-5-sonnet")
	TTS string // TTS provider/voice (e.g., "elevenlabs/rachel")
}

// WithSTTProvider manually registers an STT provider instance.
func WithSTTProvider(id string, provider stt.Provider) ClientOption {
	return func(c *Client) {
		if c.sttProviders == nil {
			c.sttProviders = make(map[string]stt.Provider)
		}
		c.sttProviders[id] = provider
	}
}

// WithTTSProvider manually registers a TTS provider instance.
func WithTTSProvider(id string, provider tts.Provider) ClientOption {
	return func(c *Client) {
		if c.ttsProviders == nil {
			c.ttsProviders = make(map[string]tts.Provider)
		}
		c.ttsProviders[id] = provider
	}
}

// WithSTTAPIKey configures an API key for a specific STT provider.
func WithSTTAPIKey(providerID, apiKey string) ClientOption {
	return func(c *Client) {
		factory, ok := GetSTTProviderFactory(providerID)
		if !ok {
			return
		}

		config := factory.DefaultConfig()
		config.APIKey = apiKey
		config.HTTPClient = c.httpClient

		if provider, err := factory.New(config); err == nil {
			if c.sttProviders == nil {
				c.sttProviders = make(map[string]stt.Provider)
			}
			c.sttProviders[providerID] = provider
		}
	}
}

// WithTTSAPIKey configures an API key for a specific TTS provider.
func WithTTSAPIKey(providerID, apiKey string) ClientOption {
	return func(c *Client) {
		factory, ok := GetTTSProviderFactory(providerID)
		if !ok {
			return
		}

		config := factory.DefaultConfig()
		config.APIKey = apiKey
		config.HTTPClient = c.httpClient

		if provider, err := factory.New(config); err == nil {
			if c.ttsProviders == nil {
				c.ttsProviders = make(map[string]tts.Provider)
			}
			c.ttsProviders[providerID] = provider
		}
	}
}

// WithDefaultVoice sets the default TTS voice for synthesis.
func WithDefaultVoice(voice string) ClientOption {
	return func(c *Client) {
		c.defaults.Voice = voice
	}
}

// WithDefaultSTT sets the default STT provider/model for transcription.
func WithDefaultSTT(sttProvider string) ClientOption {
	return func(c *Client) {
		c.defaults.STT = sttProvider
	}
}

// WithVoiceAlias defines a voice alias combining STT+LLM+TTS configuration.
func WithVoiceAlias(name string, config VoiceConfig) ClientOption {
	return func(c *Client) {
		if c.voiceAliases == nil {
			c.voiceAliases = make(map[string]VoiceConfig)
		}
		c.voiceAliases[name] = config
	}
}

// TranscribeOption configures a transcription request.
type TranscribeOption func(*transcribeOptions)

type transcribeOptions struct {
	sttProvider string
	model       string
	language    string
	diarize     bool
	timestamps  bool
}

// WithSTT sets the STT provider for transcription.
func WithSTT(provider string) TranscribeOption {
	return func(o *transcribeOptions) {
		o.sttProvider = provider
	}
}

// WithTranscribeModel sets the STT model.
func WithTranscribeModel(model string) TranscribeOption {
	return func(o *transcribeOptions) {
		o.model = model
	}
}

// WithTranscribeLanguage sets the language for transcription.
func WithTranscribeLanguage(lang string) TranscribeOption {
	return func(o *transcribeOptions) {
		o.language = lang
	}
}

// WithDiarization enables speaker diarization.
func WithDiarization() TranscribeOption {
	return func(o *transcribeOptions) {
		o.diarize = true
	}
}

// WithWordTimestamps enables word-level timestamps.
func WithWordTimestamps() TranscribeOption {
	return func(o *transcribeOptions) {
		o.timestamps = true
	}
}

// SynthesizeOption configures a synthesis request.
type SynthesizeOption func(*synthesizeOptions)

type synthesizeOptions struct {
	ttsProvider string
	voice       string
	model       string
	speed       float64
	format      tts.AudioFormat
	sampleRate  int
}

// WithVoice sets the TTS voice for synthesis.
func WithVoice(voice string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.voice = voice
	}
}

// WithTTS sets the TTS provider for synthesis.
func WithTTS(provider string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.ttsProvider = provider
	}
}

// WithSynthesizeModel sets the TTS model.
func WithSynthesizeModel(model string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.model = model
	}
}

// WithSynthesizeSpeed sets the speech speed.
func WithSynthesizeSpeed(speed float64) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.speed = speed
	}
}

// WithAudioFormat sets the output audio format.
func WithAudioFormat(format tts.AudioFormat) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.format = format
	}
}

// WithSampleRate sets the output sample rate.
func WithSampleRate(rate int) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.sampleRate = rate
	}
}

// Package vango provides the Vango AI SDK for Go.
//
// The SDK operates in two modes:
//
// # Direct Mode (Default)
//
// Runs the translation engine directly in your process. No external proxy required.
// Provider API keys come from environment variables.
//
//	client := vango.NewClient()
//
// # Proxy Mode
//
// Connects to a Vango AI Proxy instance via HTTP. Ideal for production environments
// requiring centralized governance, observability, and secret management.
//
//	client := vango.NewClient(
//	    vango.WithBaseURL("http://vango-proxy.internal:8080"),
//	    vango.WithAPIKey("vango_sk_..."),
//	)
package vango

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/providers/anthropic"
	"github.com/vango-ai/vango/pkg/core/voice"
	"github.com/vango-ai/vango/pkg/core/voice/stt"
	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

type clientMode int

const (
	modeDirect clientMode = iota
	modeProxy
)

// Client is the main entry point for the Vango AI SDK.
type Client struct {
	Messages *MessagesService
	Audio    *AudioService
	Models   *ModelsService

	// Internal
	mode       clientMode
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger
	tracer     trace.Tracer

	// Direct mode only
	core          *core.Engine
	providerKeys  map[string]string
	voicePipeline *voice.Pipeline

	// Retry configuration
	maxRetries   int
	retryBackoff time.Duration
}

// NewClient creates a new Vango AI client.
// Default is Direct Mode. Provide WithBaseURL() for Proxy Mode.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		logger:       slog.Default(),
		providerKeys: make(map[string]string),
		maxRetries:   0,
		retryBackoff: time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Determine mode
	if c.baseURL != "" {
		c.mode = modeProxy
	} else {
		c.mode = modeDirect
		c.core = core.NewEngine(c.providerKeys)
		c.initProviders()
		c.initVoicePipeline()
	}

	// Initialize services
	c.Messages = &MessagesService{client: c}
	c.Audio = &AudioService{client: c}
	c.Models = &ModelsService{client: c}

	return c
}

// initProviders registers all available providers with the engine.
func (c *Client) initProviders() {
	// Register Anthropic if API key is available
	anthropicKey := c.core.GetAPIKey("anthropic")
	if anthropicKey != "" {
		c.core.RegisterProvider(newAnthropicAdapter(anthropic.New(anthropicKey)))
	}

	// Other providers will be added in later phases:
	// - OpenAI (Phase 5)
	// - Gemini (Phase 6)
	// - Groq (Phase 7)
}

// initVoicePipeline initializes the voice pipeline if Cartesia API key is available.
func (c *Client) initVoicePipeline() {
	cartesiaKey := c.getCartesiaAPIKey()
	if cartesiaKey != "" {
		c.voicePipeline = voice.NewPipeline(cartesiaKey)
	}
}

// getCartesiaAPIKey returns the Cartesia API key from provider keys or environment.
func (c *Client) getCartesiaAPIKey() string {
	if key, ok := c.providerKeys["cartesia"]; ok && key != "" {
		return key
	}
	return os.Getenv("CARTESIA_API_KEY")
}

// getSTTProvider returns the STT provider (Cartesia).
func (c *Client) getSTTProvider() stt.Provider {
	if c.voicePipeline != nil {
		return c.voicePipeline.STTProvider()
	}
	// Create a standalone provider
	cartesiaKey := c.getCartesiaAPIKey()
	if cartesiaKey == "" {
		return nil
	}
	return stt.NewCartesia(cartesiaKey)
}

// getTTSProvider returns the TTS provider (Cartesia).
func (c *Client) getTTSProvider() tts.Provider {
	if c.voicePipeline != nil {
		return c.voicePipeline.TTSProvider()
	}
	// Create a standalone provider
	cartesiaKey := c.getCartesiaAPIKey()
	if cartesiaKey == "" {
		return nil
	}
	return tts.NewCartesia(cartesiaKey)
}

// VoicePipeline returns the voice pipeline (only available in Direct Mode with Cartesia key).
func (c *Client) VoicePipeline() *voice.Pipeline {
	return c.voicePipeline
}

// IsDirectMode returns true if the client is operating in Direct Mode.
func (c *Client) IsDirectMode() bool {
	return c.mode == modeDirect
}

// IsProxyMode returns true if the client is operating in Proxy Mode.
func (c *Client) IsProxyMode() bool {
	return c.mode == modeProxy
}

// Engine returns the core engine (only available in Direct Mode).
func (c *Client) Engine() *core.Engine {
	return c.core
}

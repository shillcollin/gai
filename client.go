package gai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
	"github.com/shillcollin/gai/stt"
	"github.com/shillcollin/gai/tts"
)

// Client is the unified entry point for AI generation across providers.
// It manages provider instances, model resolution, and request execution.
type Client struct {
	mu           sync.RWMutex
	providers    map[string]core.Provider
	sttProviders map[string]stt.Provider
	ttsProviders map[string]tts.Provider
	aliases      map[string]string
	voiceAliases map[string]VoiceConfig
	defaults     ClientDefaults
	httpClient   *http.Client
	runnerOpts   []runner.RunnerOption
}

// ClientDefaults holds default values applied to all requests.
type ClientDefaults struct {
	Model       string   // Default model if none specified (e.g., "openai/gpt-4o")
	Voice       string   // Default TTS voice (e.g., "elevenlabs/rachel")
	STT         string   // Default STT provider/model (e.g., "deepgram/nova-2")
	Temperature *float64 // Default temperature for all requests
	MaxTokens   *int     // Default max tokens for all requests
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// NewClient creates a new Client with auto-configuration from environment.
// Providers are automatically initialized if their API keys are present
// in environment variables (e.g., OPENAI_API_KEY, ANTHROPIC_API_KEY).
//
// Import providers to enable them:
//
//	import (
//	    "github.com/shillcollin/gai"
//	    _ "github.com/shillcollin/gai/providers/openai"
//	    _ "github.com/shillcollin/gai/providers/anthropic"
//	)
//
//	client := gai.NewClient()
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		providers:    make(map[string]core.Provider),
		sttProviders: make(map[string]stt.Provider),
		ttsProviders: make(map[string]tts.Provider),
		aliases:      make(map[string]string),
		voiceAliases: make(map[string]VoiceConfig),
		httpClient:   http.DefaultClient,
	}

	// Apply options first (may override providers or set HTTP client)
	for _, opt := range opts {
		opt(c)
	}

	// Auto-configure registered providers from environment
	c.autoConfigureProviders()
	c.autoConfigureSTTProviders()
	c.autoConfigureTTSProviders()

	return c
}

// autoConfigureProviders initializes providers from environment variables.
func (c *Client) autoConfigureProviders() {
	registryMu.RLock()
	defer registryMu.RUnlock()

	for name, factory := range registry {
		// Skip if already configured explicitly
		if _, exists := c.providers[name]; exists {
			continue
		}

		config := factory.DefaultConfig()
		config.HTTPClient = c.httpClient

		// Only initialize if API key is present
		if config.APIKey != "" {
			if provider, err := factory.New(config); err == nil {
				c.providers[name] = provider
			}
		}
	}
}

// autoConfigureSTTProviders initializes STT providers from environment variables.
func (c *Client) autoConfigureSTTProviders() {
	sttRegistryMu.RLock()
	defer sttRegistryMu.RUnlock()

	for name, factory := range sttRegistry {
		// Skip if already configured explicitly
		if _, exists := c.sttProviders[name]; exists {
			continue
		}

		config := factory.DefaultConfig()
		config.HTTPClient = c.httpClient

		// Only initialize if API key is present
		if config.APIKey != "" {
			if provider, err := factory.New(config); err == nil {
				c.sttProviders[name] = provider
			}
		}
	}
}

// autoConfigureTTSProviders initializes TTS providers from environment variables.
func (c *Client) autoConfigureTTSProviders() {
	ttsRegistryMu.RLock()
	defer ttsRegistryMu.RUnlock()

	for name, factory := range ttsRegistry {
		// Skip if already configured explicitly
		if _, exists := c.ttsProviders[name]; exists {
			continue
		}

		config := factory.DefaultConfig()
		config.HTTPClient = c.httpClient

		// Only initialize if API key is present
		if config.APIKey != "" {
			if provider, err := factory.New(config); err == nil {
				c.ttsProviders[name] = provider
			}
		}
	}
}

// resolveModel parses the model string and returns the provider and model ID.
// Model format: "provider/model" (e.g., "openai/gpt-4o")
func (c *Client) resolveModel(model string) (core.Provider, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if it's an alias
	if resolved, ok := c.aliases[model]; ok {
		model = resolved
	}

	// Use default if empty
	if model == "" {
		model = c.defaults.Model
	}

	if model == "" {
		return nil, "", &ModelError{Model: model, Err: ErrNoModel}
	}

	// Parse provider/model format
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return nil, "", &ModelError{
			Model:     model,
			Err:       ErrInvalidModel,
			Available: c.availableProviders(),
		}
	}

	providerID, modelID := parts[0], parts[1]

	provider, ok := c.providers[providerID]
	if !ok {
		return nil, "", &ModelError{
			Model:     model,
			Err:       ErrNoProvider,
			Available: c.availableProviders(),
		}
	}

	return provider, modelID, nil
}

// availableProviders returns a list of configured provider names.
func (c *Client) availableProviders() []string {
	names := make([]string, 0, len(c.providers))
	for name := range c.providers {
		names = append(names, name)
	}
	return names
}

// Providers returns the names of all configured providers.
func (c *Client) Providers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.availableProviders()
}

// HasProvider checks if a provider is configured.
func (c *Client) HasProvider(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.providers[name]
	return ok
}

// Generate performs a single generation request and returns the result.
// This is the universal method that handles all request types.
//
// When the request includes audio input (via Audio()) or voice output (via Voice()),
// Generate automatically handles STT transcription and/or TTS synthesis.
func (c *Client) Generate(ctx context.Context, req *RequestBuilder) (*Result, error) {
	// Check if voice pipeline is needed
	hasAudioInput := c.requestHasAudio(req)
	hasVoiceOutput := req.voiceOutput != "" || c.defaults.Voice != ""

	if hasAudioInput || hasVoiceOutput {
		return c.GenerateVoice(ctx, req)
	}

	// Standard text generation
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return nil, err
	}

	coreReq := req.build(modelID, c.defaults)

	result, err := provider.GenerateText(ctx, coreReq)
	if err != nil {
		return nil, &ProviderError{
			Provider: req.model,
			Op:       "GenerateText",
			Err:      err,
		}
	}

	return newResult(result), nil
}

// requestHasAudio checks if the request contains audio input.
func (c *Client) requestHasAudio(req *RequestBuilder) bool {
	for _, msg := range req.messages {
		for _, part := range msg.Parts {
			if _, ok := part.(core.Audio); ok {
				return true
			}
		}
	}
	return false
}

// Text is a convenience method that returns just the text response.
// Returns an error if the response contains no text.
func (c *Client) Text(ctx context.Context, req *RequestBuilder) (string, error) {
	result, err := c.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	if !result.HasText() {
		return "", ErrNoText
	}

	return result.Text(), nil
}

// Unmarshal generates structured output and unmarshals it into v.
// The value v should be a pointer to the desired output type.
func (c *Client) Unmarshal(ctx context.Context, req *RequestBuilder, v any) error {
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return err
	}

	coreReq := req.build(modelID, c.defaults)

	rawResult, err := provider.GenerateObject(ctx, coreReq)
	if err != nil {
		return &ProviderError{
			Provider: req.model,
			Op:       "GenerateObject",
			Err:      err,
		}
	}

	if err := json.Unmarshal(rawResult.JSON, v); err != nil {
		return fmt.Errorf("unmarshal structured output: %w", err)
	}

	return nil
}

// Stream streams generation events from the model.
// The returned Stream provides access to events as they arrive.
func (c *Client) Stream(ctx context.Context, req *RequestBuilder) (*Stream, error) {
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return nil, err
	}

	coreReq := req.build(modelID, c.defaults)

	coreStream, err := provider.StreamText(ctx, coreReq)
	if err != nil {
		return nil, &ProviderError{
			Provider: req.model,
			Op:       "StreamText",
			Err:      err,
		}
	}

	return newStream(coreStream), nil
}

// Run executes an agentic loop with tools until a stop condition is met.
// Tools are executed and their results fed back to the model automatically.
// Use StopWhen() or MaxSteps() on the request to control termination.
func (c *Client) Run(ctx context.Context, req *RequestBuilder) (*Result, error) {
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return nil, err
	}

	coreReq := req.build(modelID, c.defaults)

	// Create runner with provider
	r := runner.New(provider, c.runnerOpts...)

	result, err := r.ExecuteRequest(ctx, coreReq)
	if err != nil {
		return nil, &ProviderError{
			Provider: req.model,
			Op:       "Run",
			Err:      err,
		}
	}

	return newResult(result), nil
}

// StreamRun executes an agentic loop while streaming events.
// This combines the streaming interface with multi-step tool execution.
func (c *Client) StreamRun(ctx context.Context, req *RequestBuilder) (*Stream, error) {
	provider, modelID, err := c.resolveModel(req.model)
	if err != nil {
		return nil, err
	}

	coreReq := req.build(modelID, c.defaults)

	// Create runner with provider
	r := runner.New(provider, c.runnerOpts...)

	coreStream, err := r.StreamRequest(ctx, coreReq)
	if err != nil {
		return nil, &ProviderError{
			Provider: req.model,
			Op:       "StreamRun",
			Err:      err,
		}
	}

	return newStream(coreStream), nil
}

// Transcribe converts audio to text using an STT provider.
// The provider is determined by the options or defaults.
//
// Example:
//
//	text, err := client.Transcribe(ctx, audioBytes)
//	text, err := client.Transcribe(ctx, audioBytes, gai.WithSTT("deepgram/nova-2"))
func (c *Client) Transcribe(ctx context.Context, audio []byte, opts ...TranscribeOption) (string, error) {
	transcript, err := c.TranscribeFull(ctx, audio, opts...)
	if err != nil {
		return "", err
	}
	return transcript.Text, nil
}

// TranscribeFull converts audio to text and returns detailed transcription metadata.
func (c *Client) TranscribeFull(ctx context.Context, audio []byte, opts ...TranscribeOption) (*stt.Transcript, error) {
	// Apply options
	var o transcribeOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Resolve STT provider
	provider, model, err := c.resolveSTTProvider(o.sttProvider, o.model)
	if err != nil {
		return nil, err
	}

	// Build STT options
	sttOpts := stt.Options{
		Model:      model,
		Language:   o.language,
		Diarize:    o.diarize,
		Timestamps: o.timestamps,
	}

	return provider.Transcribe(ctx, audio, sttOpts)
}

// resolveSTTProvider resolves an STT provider from a provider string.
func (c *Client) resolveSTTProvider(providerStr, model string) (stt.Provider, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use default if empty
	if providerStr == "" {
		providerStr = c.defaults.STT
	}

	// If still empty, try to find any available STT provider
	if providerStr == "" {
		for _, p := range c.sttProviders {
			return p, "", nil // Return first available provider
		}
		return nil, "", fmt.Errorf("no STT provider configured")
	}

	// Parse provider/model format
	parts := strings.SplitN(providerStr, "/", 2)
	providerID := parts[0]
	if len(parts) == 2 && model == "" {
		model = parts[1]
	}

	provider, ok := c.sttProviders[providerID]
	if !ok {
		return nil, "", fmt.Errorf("STT provider %q not configured", providerID)
	}

	return provider, model, nil
}

// Synthesize converts text to audio using a TTS provider.
// The provider/voice is determined by the options or defaults.
//
// Example:
//
//	audio, err := client.Synthesize(ctx, "Hello world")
//	audio, err := client.Synthesize(ctx, "Hello", gai.WithVoice("elevenlabs/rachel"))
func (c *Client) Synthesize(ctx context.Context, text string, opts ...SynthesizeOption) ([]byte, error) {
	audio, err := c.SynthesizeFull(ctx, text, opts...)
	if err != nil {
		return nil, err
	}
	return audio.Data, nil
}

// SynthesizeFull converts text to audio and returns detailed audio metadata.
func (c *Client) SynthesizeFull(ctx context.Context, text string, opts ...SynthesizeOption) (*tts.Audio, error) {
	// Apply options
	var o synthesizeOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Resolve TTS provider
	provider, voice, err := c.resolveTTSProvider(o.ttsProvider, o.voice)
	if err != nil {
		return nil, err
	}

	// Build TTS options
	ttsOpts := tts.Options{
		Voice:      voice,
		Model:      o.model,
		Speed:      o.speed,
		Format:     o.format,
		SampleRate: o.sampleRate,
	}

	return provider.Synthesize(ctx, text, ttsOpts)
}

// StreamSynthesize streams text-to-speech conversion.
// Text is sent through the text channel and audio chunks are streamed back.
func (c *Client) StreamSynthesize(ctx context.Context, textChan <-chan string, opts ...SynthesizeOption) (*tts.AudioStream, error) {
	// Apply options
	var o synthesizeOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Resolve TTS provider
	provider, voice, err := c.resolveTTSProvider(o.ttsProvider, o.voice)
	if err != nil {
		return nil, err
	}

	// Build TTS options
	ttsOpts := tts.Options{
		Voice:      voice,
		Model:      o.model,
		Speed:      o.speed,
		Format:     o.format,
		SampleRate: o.sampleRate,
	}

	return provider.StreamSynthesize(ctx, textChan, ttsOpts)
}

// resolveTTSProvider resolves a TTS provider from a provider/voice string.
func (c *Client) resolveTTSProvider(providerStr, voice string) (tts.Provider, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use voice option if provider not specified
	if providerStr == "" && voice != "" {
		providerStr = voice
	}

	// Use default if empty
	if providerStr == "" {
		providerStr = c.defaults.Voice
	}

	// If still empty, try to find any available TTS provider
	if providerStr == "" {
		for _, p := range c.ttsProviders {
			return p, "", nil // Return first available provider
		}
		return nil, "", fmt.Errorf("no TTS provider configured")
	}

	// Parse provider/voice format
	parts := strings.SplitN(providerStr, "/", 2)
	providerID := parts[0]
	voiceID := ""
	if len(parts) == 2 {
		voiceID = parts[1]
	}

	provider, ok := c.ttsProviders[providerID]
	if !ok {
		return nil, "", fmt.Errorf("TTS provider %q not configured", providerID)
	}

	return provider, voiceID, nil
}

// STTProviders returns the names of all configured STT providers.
func (c *Client) STTProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.sttProviders))
	for name := range c.sttProviders {
		names = append(names, name)
	}
	return names
}

// TTSProviders returns the names of all configured TTS providers.
func (c *Client) TTSProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.ttsProviders))
	for name := range c.ttsProviders {
		names = append(names, name)
	}
	return names
}

// HasSTTProvider checks if an STT provider is configured.
func (c *Client) HasSTTProvider(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.sttProviders[name]
	return ok
}

// HasTTSProvider checks if a TTS provider is configured.
func (c *Client) HasTTSProvider(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.ttsProviders[name]
	return ok
}

// Conversation creates a new Conversation with automatic history management.
// Use ConversationOption functions to configure the conversation.
//
// Example:
//
//	conv := client.Conversation(
//	    gai.ConvModel("anthropic/claude-3-5-sonnet"),
//	    gai.ConvSystem("You are a helpful assistant"),
//	    gai.ConvVoice("elevenlabs/rachel"),
//	)
//
//	reply, err := conv.Say(ctx, "Hello!")
//	fmt.Println(reply.Text())
//
//	reply, err = conv.Say(ctx, "What did I just say?")
//	// Conversation history is automatically maintained
func (c *Client) Conversation(opts ...ConversationOption) *Conversation {
	conv := &Conversation{
		client:   c,
		messages: make([]core.Message, 0),
		metadata: make(map[string]any),
	}

	// Apply defaults from client
	conv.model = c.defaults.Model
	conv.voice = c.defaults.Voice
	conv.stt = c.defaults.STT

	// Apply options
	for _, opt := range opts {
		opt(conv)
	}

	return conv
}

package gai

import (
	"github.com/shillcollin/gai/core"
)

// RequestBuilder provides a fluent API for constructing generation requests.
type RequestBuilder struct {
	model        string
	messages     []core.Message
	tools        []core.ToolHandle
	toolChoice   core.ToolChoice
	outputSchema any // For structured output - stores the target type
	temperature  *float64
	maxTokens    *int
	topP         *float64
	topK         *int
	stopWhen     core.StopCondition
	onStop       core.Finalizer
	metadata     map[string]any
	providerOpts map[string]any

	// Voice pipeline fields
	voiceOutput string // TTS provider/voice for audio output (e.g., "elevenlabs/rachel")
	sttProvider string // STT provider/model for audio input (e.g., "deepgram/nova-2")
}

// Request creates a new request builder for the specified model.
// The model should be in "provider/model" format (e.g., "openai/gpt-4o").
func Request(model string) *RequestBuilder {
	return &RequestBuilder{
		model:    model,
		messages: []core.Message{},
	}
}

// System adds a system message.
func (r *RequestBuilder) System(content string) *RequestBuilder {
	r.messages = append(r.messages, core.SystemMessage(content))
	return r
}

// User adds a user message with text content.
func (r *RequestBuilder) User(content string) *RequestBuilder {
	r.messages = append(r.messages, core.UserMessage(core.Text{Text: content}))
	return r
}

// UserParts adds a user message with multiple parts.
func (r *RequestBuilder) UserParts(parts ...core.Part) *RequestBuilder {
	r.messages = append(r.messages, core.UserMessage(parts...))
	return r
}

// Assistant adds an assistant message.
func (r *RequestBuilder) Assistant(content string) *RequestBuilder {
	r.messages = append(r.messages, core.AssistantMessage(content))
	return r
}

// Messages appends multiple messages to the request.
func (r *RequestBuilder) Messages(msgs ...core.Message) *RequestBuilder {
	r.messages = append(r.messages, msgs...)
	return r
}

// Image adds an image to the last user message or creates a new user message.
func (r *RequestBuilder) Image(data []byte, mimeType string) *RequestBuilder {
	r.ensureUserMessage()
	lastMsg := &r.messages[len(r.messages)-1]
	lastMsg.Parts = append(lastMsg.Parts, core.ImagePart(data, mimeType))
	return r
}

// ImageURL adds an image URL to the last user message or creates a new user message.
func (r *RequestBuilder) ImageURL(url string) *RequestBuilder {
	r.ensureUserMessage()
	lastMsg := &r.messages[len(r.messages)-1]
	lastMsg.Parts = append(lastMsg.Parts, core.ImageURL{URL: url})
	return r
}

// Audio adds audio content to the last user message.
func (r *RequestBuilder) Audio(data []byte, mimeType string) *RequestBuilder {
	r.ensureUserMessage()
	lastMsg := &r.messages[len(r.messages)-1]
	lastMsg.Parts = append(lastMsg.Parts, core.Audio{
		Source: core.BlobRef{Kind: core.BlobBytes, Bytes: data, MIME: mimeType, Size: int64(len(data))},
	})
	return r
}

// File adds a file attachment to the last user message.
func (r *RequestBuilder) File(data []byte, mimeType, name string) *RequestBuilder {
	r.ensureUserMessage()
	lastMsg := &r.messages[len(r.messages)-1]
	lastMsg.Parts = append(lastMsg.Parts, core.File{
		Source: core.BlobRef{Kind: core.BlobBytes, Bytes: data, MIME: mimeType, Size: int64(len(data))},
		Name:   name,
	})
	return r
}

// ensureUserMessage ensures there's a user message at the end to add parts to.
func (r *RequestBuilder) ensureUserMessage() {
	if len(r.messages) == 0 || r.messages[len(r.messages)-1].Role != core.User {
		r.messages = append(r.messages, core.Message{Role: core.User, Parts: []core.Part{}})
	}
}

// Tools adds tool handles to the request.
func (r *RequestBuilder) Tools(tools ...core.ToolHandle) *RequestBuilder {
	r.tools = append(r.tools, tools...)
	return r
}

// ToolChoice sets the tool choice strategy.
func (r *RequestBuilder) ToolChoice(choice core.ToolChoice) *RequestBuilder {
	r.toolChoice = choice
	return r
}

// Schema sets the output schema for structured output.
// Pass a pointer to the target type. The schema will be derived from
// the type's structure and used to guide the model's response format.
//
// Example:
//
//	type Recipe struct {
//	    Name        string   `json:"name"`
//	    Ingredients []string `json:"ingredients"`
//	}
//	req := gai.Request("openai/gpt-4o").
//	    User("Give me a recipe").
//	    Schema(&Recipe{})
func (r *RequestBuilder) Schema(v any) *RequestBuilder {
	r.outputSchema = v
	return r
}

// OutputSchema returns the output schema if one was set.
func (r *RequestBuilder) OutputSchema() any {
	return r.outputSchema
}

// Temperature sets the sampling temperature.
func (r *RequestBuilder) Temperature(t float64) *RequestBuilder {
	r.temperature = &t
	return r
}

// MaxTokens sets the maximum number of output tokens.
func (r *RequestBuilder) MaxTokens(n int) *RequestBuilder {
	r.maxTokens = &n
	return r
}

// TopP sets the top-p (nucleus) sampling parameter.
func (r *RequestBuilder) TopP(p float64) *RequestBuilder {
	r.topP = &p
	return r
}

// TopK sets the top-k sampling parameter.
func (r *RequestBuilder) TopK(k int) *RequestBuilder {
	r.topK = &k
	return r
}

// StopWhen sets the stop condition for agentic execution (used with Run).
func (r *RequestBuilder) StopWhen(cond core.StopCondition) *RequestBuilder {
	r.stopWhen = cond
	return r
}

// MaxSteps is a convenience method that sets a MaxSteps stop condition.
func (r *RequestBuilder) MaxSteps(n int) *RequestBuilder {
	r.stopWhen = core.MaxSteps(n)
	return r
}

// OnStop sets a finalizer callback for agentic execution (used with Run).
func (r *RequestBuilder) OnStop(finalizer core.Finalizer) *RequestBuilder {
	r.onStop = finalizer
	return r
}

// Metadata adds a metadata key-value pair.
func (r *RequestBuilder) Metadata(key string, value any) *RequestBuilder {
	if r.metadata == nil {
		r.metadata = make(map[string]any)
	}
	r.metadata[key] = value
	return r
}

// Option adds a provider-specific option.
func (r *RequestBuilder) Option(key string, value any) *RequestBuilder {
	if r.providerOpts == nil {
		r.providerOpts = make(map[string]any)
	}
	r.providerOpts[key] = value
	return r
}

// Voice sets the TTS provider/voice for audio output.
// When set, the assistant's text response will be synthesized to audio.
// Format: "provider/voice" (e.g., "elevenlabs/rachel", "cartesia/sonic-english").
//
// Example:
//
//	result, err := client.Generate(ctx,
//	    gai.Request("anthropic/claude-3-5-sonnet").
//	        User("Tell me a joke").
//	        Voice("elevenlabs/rachel"))
//	audio := result.AudioData() // Synthesized speech
func (r *RequestBuilder) Voice(voice string) *RequestBuilder {
	r.voiceOutput = voice
	return r
}

// STT sets the STT provider/model for transcribing audio input.
// When set with Audio(), the audio will be transcribed using this provider
// before being sent to the LLM.
// Format: "provider/model" (e.g., "deepgram/nova-2").
//
// Example:
//
//	result, err := client.Generate(ctx,
//	    gai.Request("anthropic/claude-3-5-sonnet").
//	        Audio(speechBytes, "audio/wav").
//	        STT("deepgram/nova-2"))
//	transcript := result.Transcript() // What the user said
func (r *RequestBuilder) STT(provider string) *RequestBuilder {
	r.sttProvider = provider
	return r
}

// VoiceOutput returns the configured TTS voice, if any.
func (r *RequestBuilder) VoiceOutput() string {
	return r.voiceOutput
}

// STTProvider returns the configured STT provider, if any.
func (r *RequestBuilder) STTProvider() string {
	return r.sttProvider
}

// Model returns the model string from the builder.
func (r *RequestBuilder) Model() string {
	return r.model
}

// build converts the RequestBuilder to a core.Request.
// This is an internal method used by the Client.
func (r *RequestBuilder) build(modelID string, defaults ClientDefaults) core.Request {
	req := core.Request{
		Model:           modelID,
		Messages:        append([]core.Message{}, r.messages...),
		Tools:           append([]core.ToolHandle{}, r.tools...),
		ToolChoice:      r.toolChoice,
		StopWhen:        r.stopWhen,
		OnStop:          r.onStop,
		Metadata:        copyMap(r.metadata),
		ProviderOptions: copyMap(r.providerOpts),
	}

	// Apply builder values or defaults
	if r.temperature != nil {
		req.Temperature = float32(*r.temperature)
	} else if defaults.Temperature != nil {
		req.Temperature = float32(*defaults.Temperature)
	}

	if r.maxTokens != nil {
		req.MaxTokens = *r.maxTokens
	} else if defaults.MaxTokens != nil {
		req.MaxTokens = *defaults.MaxTokens
	}

	if r.topP != nil {
		req.TopP = float32(*r.topP)
	}

	if r.topK != nil {
		req.TopK = *r.topK
	}

	return req
}

// copyMap creates a shallow copy of a map.
func copyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

package gai

import "github.com/shillcollin/gai/core"

// ConversationOption configures a Conversation when created via Client.Conversation().
type ConversationOption func(*Conversation)

// ConvModel sets the model for the conversation.
// Format: "provider/model" (e.g., "anthropic/claude-3-5-sonnet").
func ConvModel(model string) ConversationOption {
	return func(c *Conversation) {
		c.model = model
	}
}

// ConvSystem sets the system prompt for the conversation.
func ConvSystem(system string) ConversationOption {
	return func(c *Conversation) {
		c.system = system
	}
}

// ConvVoice sets the default TTS voice for the conversation.
// Format: "provider/voice" (e.g., "elevenlabs/rachel").
func ConvVoice(voice string) ConversationOption {
	return func(c *Conversation) {
		c.voice = voice
	}
}

// ConvSTT sets the default STT provider for the conversation.
// Format: "provider/model" (e.g., "deepgram/nova-2").
func ConvSTT(provider string) ConversationOption {
	return func(c *Conversation) {
		c.stt = provider
	}
}

// ConvTools sets the default tools for the conversation.
func ConvTools(tools ...core.ToolHandle) ConversationOption {
	return func(c *Conversation) {
		c.tools = tools
	}
}

// ConvMessages initializes the conversation with existing messages.
func ConvMessages(msgs ...core.Message) ConversationOption {
	return func(c *Conversation) {
		c.messages = append(c.messages, msgs...)
	}
}

// ConvMaxMessages sets the maximum number of messages to keep in history.
// When exceeded, older messages are trimmed (system message is preserved).
// Set to 0 for unlimited (default).
func ConvMaxMessages(n int) ConversationOption {
	return func(c *Conversation) {
		c.maxMsgs = n
	}
}

// ConvMetadata sets metadata on the conversation.
func ConvMetadata(key string, value any) ConversationOption {
	return func(c *Conversation) {
		if c.metadata == nil {
			c.metadata = make(map[string]any)
		}
		c.metadata[key] = value
	}
}

// CallOption configures a single call to Say/Stream/Run/StreamRun.
type CallOption func(*callConfig)

// WithCallVoice overrides the voice for a single call.
//
// Example:
//
//	// Use a different voice just for this message
//	reply, err := conv.Say(ctx, "Hello!", gai.WithCallVoice("cartesia/sonic-english"))
func WithCallVoice(voice string) CallOption {
	return func(cfg *callConfig) {
		cfg.voice = voice
	}
}

// WithCallTools overrides the tools for a single call.
//
// Example:
//
//	// Use specific tools just for this message
//	reply, err := conv.Say(ctx, "Search for news", gai.WithCallTools(searchTool))
func WithCallTools(tools ...core.ToolHandle) CallOption {
	return func(cfg *callConfig) {
		cfg.tools = tools
	}
}

package gai

import "github.com/shillcollin/gai/core"

// Message and Role types for building conversations.
// These are type aliases from core for convenience.
type (
	// Message represents a single conversation turn.
	Message = core.Message

	// Role identifies the author of a message.
	Role = core.Role
)

// Role constants.
const (
	System    = core.System
	User      = core.User
	Assistant = core.Assistant
)

// Part types for multimodal messages.
type (
	// Part is the interface implemented by all message fragments.
	Part = core.Part

	// Text represents text content.
	Text = core.Text

	// Image references binary image content.
	Image = core.Image

	// ImageURL references remote image content.
	ImageURL = core.ImageURL

	// Audio references binary audio content.
	Audio = core.Audio

	// File references document content.
	File = core.File
)

// Tool-related types.
type (
	// ToolHandle is the interface for tool definitions.
	ToolHandle = core.ToolHandle

	// ToolChoice controls how the model uses tools.
	ToolChoice = core.ToolChoice

	// ToolCall records a model-initiated tool invocation.
	ToolCall = core.ToolCall

	// ToolResult records the response to a tool invocation.
	ToolResult = core.ToolResult

	// ToolMeta provides context to tool implementations.
	ToolMeta = core.ToolMeta
)

// ToolChoice constants.
const (
	ToolChoiceAuto     = core.ToolChoiceAuto
	ToolChoiceNone     = core.ToolChoiceNone
	ToolChoiceRequired = core.ToolChoiceRequired
)

// Stream event types.
type (
	// StreamEvent represents a streaming event from generation.
	StreamEvent = core.StreamEvent

	// EventType identifies the type of stream event.
	EventType = core.EventType
)

// Event type constants.
const (
	EventStart          = core.EventStart
	EventTextDelta      = core.EventTextDelta
	EventToolCall       = core.EventToolCall
	EventToolResult     = core.EventToolResult
	EventReasoningDelta = core.EventReasoningDelta
	EventFinish         = core.EventFinish
	EventError          = core.EventError
)

// Usage tracks token consumption.
type Usage = core.Usage

// Warning represents a non-fatal issue during processing.
type Warning = core.Warning

// Message constructors - convenience functions that wrap core functions.

// SystemMessage creates a system message with text content.
func SystemMessage(content string) Message {
	return core.SystemMessage(content)
}

// UserMessage creates a user message with the given parts.
func UserMessage(parts ...Part) Message {
	return core.UserMessage(parts...)
}

// AssistantMessage creates an assistant message with text content.
func AssistantMessage(content string) Message {
	return core.AssistantMessage(content)
}

// TextPart creates a text part for use in messages.
func TextPart(text string) Text {
	return core.Text{Text: text}
}

// ImagePart creates an inline image part from raw bytes.
func ImagePart(data []byte, mimeType string) Image {
	return core.ImagePart(data, mimeType)
}

// ImageURLPart creates an image part from a URL.
func ImageURLPart(url string) ImageURL {
	return core.ImageURL{URL: url}
}

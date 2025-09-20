package core

import "fmt"

// SystemMessage creates a system message with the given text.
func SystemMessage(text string) Message {
	return Message{Role: System, Parts: []Part{Text{Text: text}}}
}

// UserMessage creates a user message for the provided parts.
func UserMessage(parts ...Part) Message {
	clone := append([]Part(nil), parts...)
	return Message{Role: User, Parts: clone}
}

// AssistantMessage creates an assistant message with plain text.
func AssistantMessage(text string) Message {
	return Message{Role: Assistant, Parts: []Part{Text{Text: text}}}
}

// TextPart is a convenience for constructing a text part.
func TextPart(text string) Text {
	return Text{Text: text}
}

// ImagePart builds an Image part from bytes.
func ImagePart(bytes []byte, mime string) Image {
	return Image{Source: BlobRef{Kind: BlobBytes, Bytes: bytes, MIME: mime, Size: int64(len(bytes))}}
}

// ImageURLPart builds an ImageURL part.
func ImageURLPart(url string, mime string) ImageURL {
	return ImageURL{URL: url, MIME: mime}
}

// FilePart builds a File part referencing a path.
func FilePart(path string, mime string) File {
	return File{Source: BlobRef{Kind: BlobPath, Path: path, MIME: mime}}
}

// SimpleRequest creates a minimal text generation request.
func SimpleRequest(prompt string) Request {
	return Request{Messages: []Message{UserMessage(Text{Text: prompt})}}
}

// ChatRequest wraps the provided messages into a request.
func ChatRequest(messages []Message) Request {
	clone := append([]Message(nil), messages...)
	return Request{Messages: clone}
}

// ValidateMessages ensures each message has valid parts.
func ValidateMessages(messages []Message) error {
	for i, msg := range messages {
		if msg.Role == "" {
			return fmt.Errorf("message %d missing role", i)
		}
		if len(msg.Parts) == 0 {
			return fmt.Errorf("message %d missing parts", i)
		}
		for j, part := range msg.Parts {
			switch part.(type) {
			case Text, Image, ImageURL, Audio, Video, File, ToolCall, ToolResult:
				// Known part types.
			default:
				return fmt.Errorf("message %d part %d has unsupported type %T", i, j, part)
			}
		}
	}
	return nil
}

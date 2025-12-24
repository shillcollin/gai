package gai

import (
	"testing"
)

func TestRoleConstants(t *testing.T) {
	if System != "system" {
		t.Errorf("System role mismatch: got %q", System)
	}
	if User != "user" {
		t.Errorf("User role mismatch: got %q", User)
	}
	if Assistant != "assistant" {
		t.Errorf("Assistant role mismatch: got %q", Assistant)
	}
}

func TestToolChoiceConstants(t *testing.T) {
	if ToolChoiceAuto != "auto" {
		t.Errorf("ToolChoiceAuto mismatch: got %q", ToolChoiceAuto)
	}
	if ToolChoiceNone != "none" {
		t.Errorf("ToolChoiceNone mismatch: got %q", ToolChoiceNone)
	}
	if ToolChoiceRequired != "required" {
		t.Errorf("ToolChoiceRequired mismatch: got %q", ToolChoiceRequired)
	}
}

func TestEventTypeConstants(t *testing.T) {
	// Just verify they're not empty - the actual values are implementation details
	if EventTextDelta == "" {
		t.Error("EventTextDelta should not be empty")
	}
	if EventFinish == "" {
		t.Error("EventFinish should not be empty")
	}
	if EventError == "" {
		t.Error("EventError should not be empty")
	}
}

func TestSystemMessage(t *testing.T) {
	msg := SystemMessage("You are helpful")

	if msg.Role != System {
		t.Errorf("expected System role, got %s", msg.Role)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
}

func TestUserMessage(t *testing.T) {
	msg := UserMessage(TextPart("Hello"))

	if msg.Role != User {
		t.Errorf("expected User role, got %s", msg.Role)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}

	text, ok := msg.Parts[0].(Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", msg.Parts[0])
	}
	if text.Text != "Hello" {
		t.Errorf("expected 'Hello', got %q", text.Text)
	}
}

func TestAssistantMessage(t *testing.T) {
	msg := AssistantMessage("I can help")

	if msg.Role != Assistant {
		t.Errorf("expected Assistant role, got %s", msg.Role)
	}
}

func TestTextPart(t *testing.T) {
	part := TextPart("Hello world")

	if part.Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", part.Text)
	}
}

func TestImagePart(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	part := ImagePart(data, "image/png")

	if part.Source.Kind != "bytes" {
		t.Errorf("expected bytes kind, got %s", part.Source.Kind)
	}
	if part.Source.MIME != "image/png" {
		t.Errorf("expected image/png, got %s", part.Source.MIME)
	}
}

func TestImageURLPart(t *testing.T) {
	part := ImageURLPart("https://example.com/image.png")

	if part.URL != "https://example.com/image.png" {
		t.Errorf("expected URL to match, got %s", part.URL)
	}
}

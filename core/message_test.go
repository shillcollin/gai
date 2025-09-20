package core

import "testing"

func TestBlobRefValidate(t *testing.T) {
	ref := BlobRef{Kind: BlobBytes, Bytes: []byte("hello"), MIME: "text/plain"}
	if err := ref.Validate(); err != nil {
		t.Fatalf("expected valid blob: %v", err)
	}

	bad := BlobRef{Kind: BlobBytes, MIME: "text/plain"}
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestValidateMessages(t *testing.T) {
	msg := Message{Role: User, Parts: []Part{Text{Text: "hi"}}}
	if err := ValidateMessages([]Message{msg}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invalid := Message{Role: User}
	if err := ValidateMessages([]Message{invalid}); err == nil {
		t.Fatalf("expected error")
	}
}

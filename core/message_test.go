package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

func TestBlobRefReadBytes(t *testing.T) {
	ref := BlobRef{Kind: BlobBytes, Bytes: []byte("sample"), MIME: "text/plain"}
	data, err := ref.Read()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "sample" {
		t.Fatalf("unexpected data %q", data)
	}
	// Mutate returned slice and ensure original retained for future reads.
	data[0] = 'S'
	again, err := ref.Read()
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if string(again) != "sample" {
		t.Fatalf("expected defensive copy, got %q", again)
	}
}

func TestBlobRefStreamPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	ref := BlobRef{Kind: BlobPath, Path: path, MIME: "application/octet-stream"}
	reader, err := ref.Stream()
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("unexpected data %q", data)
	}
}

func TestBlobRefStreamURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		_, _ = w.Write([]byte("remote"))
	}))
	defer srv.Close()

	ref := BlobRef{Kind: BlobURL, URL: srv.URL, MIME: "text/plain"}
	data, err := ref.Read()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "remote" {
		t.Fatalf("unexpected data %q", data)
	}
}

func TestBlobRefStreamURLStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing"))
	}))
	defer srv.Close()

	ref := BlobRef{Kind: BlobURL, URL: srv.URL, MIME: "text/plain"}
	if _, err := ref.Stream(); err == nil {
		t.Fatalf("expected error for non-200 response")
	}
}

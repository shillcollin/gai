package core

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Role identifies the author of a message.
type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
)

// Message represents a single conversation turn.
type Message struct {
	Role     Role           `json:"role"`
	Parts    []Part         `json:"parts"`
	Name     string         `json:"name,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PartType identifies the type of content stored in a Part.
type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeImage      PartType = "image"
	PartTypeImageURL   PartType = "image_url"
	PartTypeAudio      PartType = "audio"
	PartTypeVideo      PartType = "video"
	PartTypeFile       PartType = "file"
	PartTypeToolCall   PartType = "tool_call"
	PartTypeToolResult PartType = "tool_result"
)

// Part is the interface implemented by all message fragments.
type Part interface {
	Type() PartType
	Content() any
}

// Text represents text content.
type Text struct {
	Text string `json:"text"`
}

func (t Text) Type() PartType { return PartTypeText }
func (t Text) Content() any   { return t.Text }

// Image references binary image content.
type Image struct {
	Source BlobRef `json:"source"`
	Alt    string  `json:"alt,omitempty"`
}

func (i Image) Type() PartType { return PartTypeImage }
func (i Image) Content() any   { return i.Source }

// ImageURL references remote image content.
type ImageURL struct {
	URL    string `json:"url"`
	MIME   string `json:"mime,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (i ImageURL) Type() PartType { return PartTypeImageURL }
func (i ImageURL) Content() any   { return i.URL }

// Audio references binary audio content.
type Audio struct {
	Source   BlobRef `json:"source"`
	Format   string  `json:"format,omitempty"`
	Duration int64   `json:"duration,omitempty"`
}

func (a Audio) Type() PartType { return PartTypeAudio }
func (a Audio) Content() any   { return a.Source }

// Video references binary video content.
type Video struct {
	Source   BlobRef `json:"source"`
	Format   string  `json:"format,omitempty"`
	Duration int64   `json:"duration,omitempty"`
}

func (v Video) Type() PartType { return PartTypeVideo }
func (v Video) Content() any   { return v.Source }

// File references document content for retrieval-augmented use cases.
type File struct {
	Source BlobRef `json:"source"`
	Name   string  `json:"name,omitempty"`
}

func (f File) Type() PartType { return PartTypeFile }
func (f File) Content() any   { return f.Source }

// ToolCall records a model-initiated tool invocation.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (t ToolCall) Type() PartType { return PartTypeToolCall }
func (t ToolCall) Content() any   { return t }

// ToolResult records the application-provided response to a tool invocation.
type ToolResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func (t ToolResult) Type() PartType { return PartTypeToolResult }
func (t ToolResult) Content() any   { return t }

// BlobKind identifies how binary data should be fetched.
type BlobKind string

const (
	BlobBytes    BlobKind = "bytes"
	BlobPath     BlobKind = "path"
	BlobURL      BlobKind = "url"
	BlobProvider BlobKind = "provider"
)

// BlobRef points to binary data without forcing immediate loading.
type BlobRef struct {
	Kind BlobKind `json:"kind"`

	Bytes      []byte `json:"bytes,omitempty"`
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`

	MIME string `json:"mime"`
	Size int64  `json:"size,omitempty"`
	Hash string `json:"hash,omitempty"`
}

// Validate ensures the blob reference is well-formed.
func (b BlobRef) Validate() error {
	if b.Kind == "" {
		return errors.New("blob kind is required")
	}
	if b.MIME == "" {
		return errors.New("blob MIME type is required")
	}
	switch b.Kind {
	case BlobBytes:
		if len(b.Bytes) == 0 {
			return errors.New("bytes kind requires data")
		}
	case BlobPath:
		if b.Path == "" {
			return errors.New("path kind requires path")
		}
	case BlobURL:
		if b.URL == "" {
			return errors.New("url kind requires URL")
		}
	case BlobProvider:
		if b.ProviderID == "" {
			return errors.New("provider kind requires provider ID")
		}
	default:
		return fmt.Errorf("unknown blob kind: %s", b.Kind)
	}
	return nil
}

// Base64 returns a base64 representation of the binary data when available.
func (b BlobRef) Base64() (string, error) {
	switch b.Kind {
	case BlobBytes:
		return base64.StdEncoding.EncodeToString(b.Bytes), nil
	default:
		return "", fmt.Errorf("base64 conversion unsupported for kind %s", b.Kind)
	}
}

// Read materializes the blob contents into memory. Callers should prefer Stream when
// dealing with large payloads.
func (b BlobRef) Read() ([]byte, error) {
	reader, err := b.Stream()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Stream returns an io.ReadCloser for the blob contents. The caller is responsible
// for closing the returned reader. Network-based blobs use the default HTTP client.
func (b BlobRef) Stream() (io.ReadCloser, error) {
	switch b.Kind {
	case BlobBytes:
		// Return a defensive copy-backed reader to prevent mutation while streaming.
		buf := make([]byte, len(b.Bytes))
		copy(buf, b.Bytes)
		return io.NopCloser(bytes.NewReader(buf)), nil
	case BlobPath:
		if b.Path == "" {
			return nil, errors.New("blob path is empty")
		}
		return os.Open(b.Path)
	case BlobURL:
		if b.URL == "" {
			return nil, errors.New("blob url is empty")
		}
		resp, err := http.Get(b.URL)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			defer resp.Body.Close()
			return nil, fmt.Errorf("blob url returned status %s", resp.Status)
		}
		return resp.Body, nil
	case BlobProvider:
		return nil, errors.New("provider-managed blobs must be resolved by the provider")
	default:
		return nil, fmt.Errorf("unsupported blob kind %q", b.Kind)
	}
}

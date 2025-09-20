package core

import "context"

// Provider is the primary interface implemented by all model adapters. It exposes
// text generation (batch and streaming) plus structured outputs while remaining
// provider-agnostic.
type Provider interface {
	GenerateText(ctx context.Context, req Request) (*TextResult, error)
	StreamText(ctx context.Context, req Request) (*Stream, error)
	GenerateObject(ctx context.Context, req Request) (*ObjectResultRaw, error)
	StreamObject(ctx context.Context, req Request) (*ObjectStreamRaw, error)
	Capabilities() Capabilities
}

// Capabilities describes the features supported by a provider.
type Capabilities struct {
	Streaming         bool
	ParallelToolCalls bool
	StrictJSON        bool

	Images bool
	Audio  bool
	Video  bool
	Files  bool

	Reasoning bool
	Citations bool
	Safety    bool
	Sessions  bool

	MaxInputTokens  int
	MaxOutputTokens int
	MaxFileSize     int64
	MaxToolCalls    int

	Provider string
	Models   []string
}

package vango

import (
	"context"

	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/providers/anthropic"
	"github.com/vango-ai/vango/pkg/core/types"
)

// anthropicAdapter adapts the anthropic.Provider to the core.Provider interface.
// This is necessary because the anthropic package defines its own types to avoid import cycles.
type anthropicAdapter struct {
	provider *anthropic.Provider
}

// newAnthropicAdapter creates a new anthropic adapter.
func newAnthropicAdapter(p *anthropic.Provider) *anthropicAdapter {
	return &anthropicAdapter{provider: p}
}

// Name returns the provider identifier.
func (a *anthropicAdapter) Name() string {
	return a.provider.Name()
}

// CreateMessage sends a non-streaming request.
func (a *anthropicAdapter) CreateMessage(ctx context.Context, req *types.MessageRequest) (*types.MessageResponse, error) {
	return a.provider.CreateMessage(ctx, req)
}

// StreamMessage sends a streaming request.
func (a *anthropicAdapter) StreamMessage(ctx context.Context, req *types.MessageRequest) (core.EventStream, error) {
	es, err := a.provider.StreamMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &eventStreamAdapter{es: es}, nil
}

// Capabilities returns what this provider supports.
func (a *anthropicAdapter) Capabilities() core.ProviderCapabilities {
	caps := a.provider.Capabilities()
	return core.ProviderCapabilities{
		Vision:           caps.Vision,
		AudioInput:       caps.AudioInput,
		AudioOutput:      caps.AudioOutput,
		Video:            caps.Video,
		Tools:            caps.Tools,
		ToolStreaming:    caps.ToolStreaming,
		Thinking:         caps.Thinking,
		StructuredOutput: caps.StructuredOutput,
		NativeTools:      caps.NativeTools,
	}
}

// eventStreamAdapter adapts the anthropic.EventStream to core.EventStream.
type eventStreamAdapter struct {
	es anthropic.EventStream
}

func (e *eventStreamAdapter) Next() (types.StreamEvent, error) {
	return e.es.Next()
}

func (e *eventStreamAdapter) Close() error {
	return e.es.Close()
}

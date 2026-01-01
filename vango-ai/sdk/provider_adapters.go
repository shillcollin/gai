package vango

import (
	"context"

	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/providers/anthropic"
	"github.com/vango-ai/vango/pkg/core/providers/cerebras"
	"github.com/vango-ai/vango/pkg/core/providers/groq"
	"github.com/vango-ai/vango/pkg/core/providers/openai"
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

// --- OpenAI Adapter ---

// openaiAdapter adapts the openai.Provider to the core.Provider interface.
type openaiAdapter struct {
	provider *openai.Provider
}

// newOpenAIAdapter creates a new OpenAI adapter.
func newOpenAIAdapter(p *openai.Provider) *openaiAdapter {
	return &openaiAdapter{provider: p}
}

// Name returns the provider identifier.
func (a *openaiAdapter) Name() string {
	return a.provider.Name()
}

// CreateMessage sends a non-streaming request.
func (a *openaiAdapter) CreateMessage(ctx context.Context, req *types.MessageRequest) (*types.MessageResponse, error) {
	return a.provider.CreateMessage(ctx, req)
}

// StreamMessage sends a streaming request.
func (a *openaiAdapter) StreamMessage(ctx context.Context, req *types.MessageRequest) (core.EventStream, error) {
	es, err := a.provider.StreamMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &openaiEventStreamAdapter{es: es}, nil
}

// Capabilities returns what this provider supports.
func (a *openaiAdapter) Capabilities() core.ProviderCapabilities {
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

// openaiEventStreamAdapter adapts the openai.EventStream to core.EventStream.
type openaiEventStreamAdapter struct {
	es openai.EventStream
}

func (e *openaiEventStreamAdapter) Next() (types.StreamEvent, error) {
	return e.es.Next()
}

func (e *openaiEventStreamAdapter) Close() error {
	return e.es.Close()
}

// --- Groq Adapter ---

// groqAdapter adapts the groq.Provider to the core.Provider interface.
type groqAdapter struct {
	provider *groq.Provider
}

// newGroqAdapter creates a new Groq adapter.
func newGroqAdapter(p *groq.Provider) *groqAdapter {
	return &groqAdapter{provider: p}
}

// Name returns the provider identifier.
func (a *groqAdapter) Name() string {
	return a.provider.Name()
}

// CreateMessage sends a non-streaming request.
func (a *groqAdapter) CreateMessage(ctx context.Context, req *types.MessageRequest) (*types.MessageResponse, error) {
	return a.provider.CreateMessage(ctx, req)
}

// StreamMessage sends a streaming request.
func (a *groqAdapter) StreamMessage(ctx context.Context, req *types.MessageRequest) (core.EventStream, error) {
	es, err := a.provider.StreamMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &groqEventStreamAdapter{es: es}, nil
}

// Capabilities returns what this provider supports.
func (a *groqAdapter) Capabilities() core.ProviderCapabilities {
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

// groqEventStreamAdapter adapts the groq.EventStream to core.EventStream.
type groqEventStreamAdapter struct {
	es groq.EventStream
}

func (e *groqEventStreamAdapter) Next() (types.StreamEvent, error) {
	return e.es.Next()
}

func (e *groqEventStreamAdapter) Close() error {
	return e.es.Close()
}

// --- Cerebras Adapter ---

// cerebrasAdapter adapts the cerebras.Provider to the core.Provider interface.
type cerebrasAdapter struct {
	provider *cerebras.Provider
}

// newCerebrasAdapter creates a new Cerebras adapter.
func newCerebrasAdapter(p *cerebras.Provider) *cerebrasAdapter {
	return &cerebrasAdapter{provider: p}
}

// Name returns the provider identifier.
func (a *cerebrasAdapter) Name() string {
	return a.provider.Name()
}

// CreateMessage sends a non-streaming request.
func (a *cerebrasAdapter) CreateMessage(ctx context.Context, req *types.MessageRequest) (*types.MessageResponse, error) {
	return a.provider.CreateMessage(ctx, req)
}

// StreamMessage sends a streaming request.
func (a *cerebrasAdapter) StreamMessage(ctx context.Context, req *types.MessageRequest) (core.EventStream, error) {
	es, err := a.provider.StreamMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &cerebrasEventStreamAdapter{es: es}, nil
}

// Capabilities returns what this provider supports.
func (a *cerebrasAdapter) Capabilities() core.ProviderCapabilities {
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

// cerebrasEventStreamAdapter adapts the cerebras.EventStream to core.EventStream.
type cerebrasEventStreamAdapter struct {
	es cerebras.EventStream
}

func (e *cerebrasEventStreamAdapter) Next() (types.StreamEvent, error) {
	return e.es.Next()
}

func (e *cerebrasEventStreamAdapter) Close() error {
	return e.es.Close()
}

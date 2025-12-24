package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/shillcollin/gai/core"
)

// MockProvider is a configurable mock implementation of core.Provider for testing.
type MockProvider struct {
	mu sync.Mutex

	// Configurable responses
	TextResponse   *core.TextResult
	ObjectResponse *core.ObjectResultRaw
	StreamResponse *core.Stream
	ObjectStream   *core.ObjectStreamRaw
	Caps           core.Capabilities

	// Error injection
	TextErr   error
	ObjectErr error
	StreamErr error

	// Call tracking
	TextCalls   []core.Request
	ObjectCalls []core.Request
	StreamCalls []core.Request

	// Custom handlers (override default behavior)
	OnGenerateText   func(ctx context.Context, req core.Request) (*core.TextResult, error)
	OnStreamText     func(ctx context.Context, req core.Request) (*core.Stream, error)
	OnGenerateObject func(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error)
	OnStreamObject   func(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error)
}

// NewMockProvider creates a new MockProvider with sensible defaults.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		TextResponse: &core.TextResult{
			Text:     "mock response",
			Model:    "mock-model",
			Provider: "mock",
			Usage: core.Usage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
			FinishReason: core.StopReason{Type: core.StopReasonComplete},
		},
		ObjectResponse: &core.ObjectResultRaw{
			JSON: json.RawMessage(`{"result": "mock"}`),
			Usage: core.Usage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		},
		Caps: core.Capabilities{
			Streaming:         true,
			ParallelToolCalls: true,
			StrictJSON:        true,
			Images:            true,
			Audio:             true,
			Files:             true,
			Provider:          "mock",
			Models:            []string{"mock-model"},
		},
	}
}

// GenerateText implements core.Provider.
func (m *MockProvider) GenerateText(ctx context.Context, req core.Request) (*core.TextResult, error) {
	m.mu.Lock()
	m.TextCalls = append(m.TextCalls, req)
	m.mu.Unlock()

	if m.OnGenerateText != nil {
		return m.OnGenerateText(ctx, req)
	}

	if m.TextErr != nil {
		return nil, m.TextErr
	}

	return m.TextResponse, nil
}

// StreamText implements core.Provider.
func (m *MockProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	m.mu.Lock()
	m.StreamCalls = append(m.StreamCalls, req)
	m.mu.Unlock()

	if m.OnStreamText != nil {
		return m.OnStreamText(ctx, req)
	}

	if m.StreamErr != nil {
		return nil, m.StreamErr
	}

	// Return a simple stream that immediately completes
	if m.StreamResponse != nil {
		return m.StreamResponse, nil
	}

	// Create a simple mock stream
	return NewMockStream(ctx, m.TextResponse.Text), nil
}

// GenerateObject implements core.Provider.
func (m *MockProvider) GenerateObject(ctx context.Context, req core.Request) (*core.ObjectResultRaw, error) {
	m.mu.Lock()
	m.ObjectCalls = append(m.ObjectCalls, req)
	m.mu.Unlock()

	if m.OnGenerateObject != nil {
		return m.OnGenerateObject(ctx, req)
	}

	if m.ObjectErr != nil {
		return nil, m.ObjectErr
	}

	return m.ObjectResponse, nil
}

// StreamObject implements core.Provider.
func (m *MockProvider) StreamObject(ctx context.Context, req core.Request) (*core.ObjectStreamRaw, error) {
	m.mu.Lock()
	m.ObjectCalls = append(m.ObjectCalls, req)
	m.mu.Unlock()

	if m.OnStreamObject != nil {
		return m.OnStreamObject(ctx, req)
	}

	if m.ObjectErr != nil {
		return nil, m.ObjectErr
	}

	return m.ObjectStream, nil
}

// Capabilities implements core.Provider.
func (m *MockProvider) Capabilities() core.Capabilities {
	return m.Caps
}

// Reset clears all tracked calls.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TextCalls = nil
	m.ObjectCalls = nil
	m.StreamCalls = nil
}

// SetTextResponse configures the text response.
func (m *MockProvider) SetTextResponse(text string) {
	m.TextResponse = &core.TextResult{
		Text:     text,
		Model:    "mock-model",
		Provider: "mock",
		Usage: core.Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
		FinishReason: core.StopReason{Type: core.StopReasonComplete},
	}
}

// SetObjectResponse configures the object response.
func (m *MockProvider) SetObjectResponse(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.ObjectResponse = &core.ObjectResultRaw{
		JSON: data,
		Usage: core.Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}
	return nil
}

// NewMockStream creates a mock stream that emits the given text.
func NewMockStream(ctx context.Context, text string) *core.Stream {
	s := core.NewStream(ctx, 2)

	go func() {
		defer s.Close()
		s.Push(core.StreamEvent{
			Type:      core.EventTextDelta,
			TextDelta: text,
		})
		s.Push(core.StreamEvent{
			Type: core.EventFinish,
		})
	}()

	return s
}

// StepConfig configures a single step response for multi-step testing.
type StepConfig struct {
	Text      string
	ToolCalls []core.ToolCall
	Usage     core.Usage
}

// NewMockStreamWithToolCalls creates a mock stream that emits text and tool calls.
func NewMockStreamWithToolCalls(ctx context.Context, text string, toolCalls []core.ToolCall, usage core.Usage) *core.Stream {
	s := core.NewStream(ctx, len(toolCalls)+3)

	go func() {
		defer s.Close()
		// Emit text first
		if text != "" {
			s.Push(core.StreamEvent{
				Type:      core.EventTextDelta,
				TextDelta: text,
			})
		}
		// Emit tool calls
		for _, tc := range toolCalls {
			s.Push(core.StreamEvent{
				Type:     core.EventToolCall,
				ToolCall: tc,
			})
		}
		// Emit finish
		s.Push(core.StreamEvent{
			Type:  core.EventFinish,
			Usage: usage,
		})
	}()

	return s
}

// MultiStepMockProvider helps test agentic loops by configuring multi-step responses.
type MultiStepMockProvider struct {
	*MockProvider
	steps       []StepConfig
	currentStep int
	mu          sync.Mutex
}

// NewMultiStepMockProvider creates a mock provider that supports multiple configured steps.
func NewMultiStepMockProvider(steps []StepConfig) *MultiStepMockProvider {
	return &MultiStepMockProvider{
		MockProvider: NewMockProvider(),
		steps:        steps,
		currentStep:  0,
	}
}

// StreamText returns the next configured step response.
func (m *MultiStepMockProvider) StreamText(ctx context.Context, req core.Request) (*core.Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StreamCalls = append(m.StreamCalls, req)

	if m.StreamErr != nil {
		return nil, m.StreamErr
	}

	if m.currentStep >= len(m.steps) {
		// Return empty response (no tool calls) to end the loop
		return NewMockStream(ctx, "Final response"), nil
	}

	step := m.steps[m.currentStep]
	m.currentStep++

	return NewMockStreamWithToolCalls(ctx, step.Text, step.ToolCalls, step.Usage), nil
}

// ResetSteps resets the step counter for re-use.
func (m *MultiStepMockProvider) ResetSteps() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStep = 0
}

// SimpleMemo is a basic in-memory ToolMemo implementation for testing.
type SimpleMemo struct {
	mu    sync.Mutex
	cache map[string]any
}

// NewSimpleMemo creates a new SimpleMemo for testing.
func NewSimpleMemo() *SimpleMemo {
	return &SimpleMemo{
		cache: make(map[string]any),
	}
}

// Get retrieves a cached result.
func (m *SimpleMemo) Get(name string, inputHash string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + ":" + inputHash
	result, ok := m.cache[key]
	return result, ok
}

// Set stores a result in the cache.
func (m *SimpleMemo) Set(name string, inputHash string, result any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + ":" + inputHash
	m.cache[key] = result
}

package adapters

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/live"
	"github.com/vango-ai/vango/pkg/core/types"
)

// EngineRunStreamAdapter adapts core.Engine streaming into live.RunStreamInterface.
// It supports stopping the current response (without injecting a message) and
// injecting new user turns while maintaining conversation history.
type EngineRunStreamAdapter struct {
	engine *core.Engine
	config *live.SessionConfig
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	events chan live.RunStreamEvent
	done   chan struct{}
	closed atomic.Bool

	mu       sync.Mutex
	messages []types.Message

	turnCancel context.CancelFunc
	turnDone   chan struct{}
	turnActive bool
}

type EngineRunStreamAdapterConfig struct {
	Engine        *core.Engine
	SessionConfig *live.SessionConfig
	FirstMessage  string
	Logger        *slog.Logger
}

func NewEngineRunStreamAdapter(ctx context.Context, cfg EngineRunStreamAdapterConfig) (*EngineRunStreamAdapter, error) {
	ctx, cancel := context.WithCancel(ctx)

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	a := &EngineRunStreamAdapter{
		engine: cfg.Engine,
		config: cfg.SessionConfig,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		events: make(chan live.RunStreamEvent, 200),
		done:   make(chan struct{}),
		messages: []types.Message{
			{Role: "user", Content: cfg.FirstMessage},
		},
	}

	a.startTurnLocked()

	return a, nil
}

func (a *EngineRunStreamAdapter) Events() <-chan live.RunStreamEvent {
	return a.events
}

func (a *EngineRunStreamAdapter) StopResponse(partialText string, behavior live.InterruptBehavior) error {
	turnDone, stopped := a.stopTurn()
	if stopped && turnDone != nil {
		<-turnDone
	}

	a.savePartialAssistant(partialText, behavior)

	// Notify session that output should be reset.
	a.emitEvent(&live.InterruptedEvent{})

	return nil
}

func (a *EngineRunStreamAdapter) Interrupt(msg live.UserMessage, behavior live.InterruptBehavior) error {
	// Stop current response (if any) and optionally save partial assistant output.
	turnDone, stopped := a.stopTurn()
	if stopped && turnDone != nil {
		<-turnDone
	}

	// Save partial assistant output using what the caller provided (if any).
	// If the caller doesn't have a snapshot, they can pass "" and behavior Save/Marked
	// will be a no-op.
	a.savePartialAssistant("", behavior)

	a.mu.Lock()
	a.messages = append(a.messages, types.Message{
		Role:    msg.Role,
		Content: msg.Content,
	})
	a.startTurnLocked()
	a.mu.Unlock()

	a.emitEvent(&live.InterruptedEvent{})

	return nil
}

func (a *EngineRunStreamAdapter) InterruptWithText(text string) error {
	return a.Interrupt(live.UserMessage{Role: "user", Content: text}, live.InterruptDiscard)
}

func (a *EngineRunStreamAdapter) Cancel() error {
	_, _ = a.stopTurn()
	a.emitEvent(&live.InterruptedEvent{})
	return nil
}

func (a *EngineRunStreamAdapter) Close() error {
	if a.closed.Swap(true) {
		return nil
	}
	a.cancel()

	a.mu.Lock()
	turnDone := a.turnDone
	turnCancel := a.turnCancel
	a.turnCancel = nil
	a.turnDone = nil
	a.turnActive = false
	a.mu.Unlock()

	if turnCancel != nil {
		turnCancel()
	}
	if turnDone != nil {
		<-turnDone
	}

	close(a.done)
	close(a.events)
	return nil
}

func (a *EngineRunStreamAdapter) stopTurn() (turnDone chan struct{}, stopped bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.turnActive {
		return nil, false
	}
	turnCancel := a.turnCancel
	turnDone = a.turnDone
	a.turnCancel = nil
	a.turnDone = nil
	a.turnActive = false

	if turnCancel != nil {
		turnCancel()
	}

	return turnDone, true
}

func (a *EngineRunStreamAdapter) savePartialAssistant(partialText string, behavior live.InterruptBehavior) {
	partialText = strings.TrimSpace(partialText)
	if partialText == "" {
		return
	}

	switch behavior {
	case live.InterruptDiscard:
		return
	case live.InterruptSavePartial:
		// Save as-is.
	case live.InterruptSaveMarked:
		partialText = partialText + " [interrupted]"
	default:
		// Default to discard for unknown behaviors.
		return
	}

	a.mu.Lock()
	a.messages = append(a.messages, types.Message{
		Role:    "assistant",
		Content: partialText,
	})
	a.mu.Unlock()
}

func (a *EngineRunStreamAdapter) startTurnLocked() {
	if a.closed.Load() {
		return
	}

	// Ensure only one active turn.
	if a.turnActive {
		return
	}

	turnCtx, turnCancel := context.WithCancel(a.ctx)
	turnDone := make(chan struct{})

	a.turnCancel = turnCancel
	a.turnDone = turnDone
	a.turnActive = true

	messagesCopy := make([]types.Message, len(a.messages))
	copy(messagesCopy, a.messages)

	config := a.config
	engine := a.engine

	go a.streamTurn(turnCtx, turnDone, engine, config, messagesCopy)
}

func (a *EngineRunStreamAdapter) streamTurn(
	ctx context.Context,
	turnDone chan struct{},
	engine *core.Engine,
	config *live.SessionConfig,
	messages []types.Message,
) {
	defer close(turnDone)
	defer func() {
		a.mu.Lock()
		if a.turnDone == turnDone {
			a.turnDone = nil
			a.turnCancel = nil
			a.turnActive = false
		}
		a.mu.Unlock()
	}()

	req := &types.MessageRequest{
		Model:    config.Model,
		System:   config.System,
		Messages: messages,
		Stream:   true,
	}

	if len(config.Tools) > 0 {
		for _, tool := range config.Tools {
			if t, ok := tool.(types.Tool); ok {
				req.Tools = append(req.Tools, t)
			}
		}
	}

	stream, err := engine.StreamMessage(ctx, req)
	if err != nil {
		a.emitEvent(&live.RunErrorEvent{Err: err})
		return
	}
	defer stream.Close()

	a.emitEvent(&live.StepStartEvent{})

	var (
		responseContent []types.ContentBlock
		stopReason      types.StopReason
	)

	for {
		select {
		case <-ctx.Done():
			// Ensure we don't append a partial assistant message here; caller decides.
			a.emitEvent(&live.StepCompleteEvent{})
			return
		default:
		}

		event, err := stream.Next()
		if err != nil {
			if err.Error() != "EOF" {
				a.emitEvent(&live.RunErrorEvent{Err: err})
			}
			break
		}
		if event == nil {
			break
		}

		a.emitEvent(&live.StreamEventWrapper{Evt: event})

		switch e := event.(type) {
		case types.ContentBlockStartEvent:
			for len(responseContent) <= e.Index {
				responseContent = append(responseContent, nil)
			}
			responseContent[e.Index] = e.ContentBlock
		case types.ContentBlockDeltaEvent:
			if e.Index < len(responseContent) {
				responseContent[e.Index] = applyDelta(responseContent[e.Index], e.Delta)
			}
		case types.MessageDeltaEvent:
			stopReason = e.Delta.StopReason
		}
	}

	if ctx.Err() == nil && len(responseContent) > 0 {
		a.mu.Lock()
		a.messages = append(a.messages, types.Message{
			Role:    "assistant",
			Content: responseContent,
		})
		a.mu.Unlock()

		if stopReason == "tool_use" {
			for _, block := range responseContent {
				if toolUse, ok := block.(types.ToolUseBlock); ok {
					a.emitEvent(&live.ToolCallStartEvent{
						ID:       toolUse.ID,
						Name:     toolUse.Name,
						InputMap: toolUse.Input,
					})
				}
			}
		}
	}

	a.emitEvent(&live.StepCompleteEvent{})
}

func (a *EngineRunStreamAdapter) emitEvent(event live.RunStreamEvent) {
	if a.closed.Load() {
		return
	}
	select {
	case a.events <- event:
	case <-a.ctx.Done():
	default:
	}
}

func applyDelta(block types.ContentBlock, delta types.Delta) types.ContentBlock {
	switch d := delta.(type) {
	case types.TextDelta:
		if tb, ok := block.(types.TextBlock); ok {
			tb.Text += d.Text
			return tb
		}
		if tb, ok := block.(*types.TextBlock); ok {
			tb.Text += d.Text
			return tb
		}
	case types.ThinkingDelta:
		if tb, ok := block.(types.ThinkingBlock); ok {
			tb.Thinking += d.Thinking
			return tb
		}
		if tb, ok := block.(*types.ThinkingBlock); ok {
			tb.Thinking += d.Thinking
			return tb
		}
	}
	return block
}

var _ live.RunStreamInterface = (*EngineRunStreamAdapter)(nil)

package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/live"
	"github.com/vango-ai/vango/pkg/core/types"
	"github.com/vango-ai/vango/pkg/core/voice/stt"
)

// handleLive handles the /v1/messages/live WebSocket endpoint.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Authenticate (WebSocket auth via query param or header)
	keyConfig, ok := s.auth.AuthenticateWebSocket(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "authentication_error", "Invalid or missing API key")
		return
	}

	// Check rate limit for concurrent sessions
	currentSessions := int(s.sessionCount.Load())
	if !s.rateLimiter.CheckLiveSessionLimit(currentSessions) {
		s.writeError(w, http.StatusTooManyRequests, "rate_limit_error",
			fmt.Sprintf("Maximum concurrent sessions (%d) reached", s.config.RateLimit.MaxConcurrentSessions))
		return
	}

	// Upgrade to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	// Create session handler
	handler := &liveSessionHandler{
		server:    s,
		conn:      conn,
		userID:    keyConfig.UserID,
		keyName:   keyConfig.Name,
		logger:    s.logger.With("session_type", "live", "user_id", keyConfig.UserID),
		startTime: start,
		provider:  "",
		model:     "",
	}

	// Run the session
	handler.run(r.Context())
}

// liveSessionHandler manages a single live WebSocket session.
type liveSessionHandler struct {
	server    *Server
	conn      *websocket.Conn
	session   *live.LiveSession
	userID    string
	keyName   string
	logger    *slog.Logger
	startTime time.Time
	provider  string
	model     string
}

// run executes the live session lifecycle.
func (h *liveSessionHandler) run(ctx context.Context) {
	defer h.cleanup()

	// Track session
	h.server.sessionCount.Add(1)
	h.server.metrics.RecordLiveSessionStart()

	h.logger.Info("live session started")

	// Create the session configuration
	sessionCfg := live.LiveSessionConfig{
		Connection:       h.conn,
		RunStreamCreator: h.createRunStreamCreator(),
		STTFactory:       h.createSTTFactory(),
		LLMFunc:          h.createLLMFunc(),
	}

	// Create the session
	h.session = live.NewLiveSession(sessionCfg)

	// Store in server map
	sessionID := h.session.ID()
	h.server.liveSessionsMu.Lock()
	h.server.liveSessions[sessionID] = h.session
	h.server.liveSessionsMu.Unlock()

	// Start session processing
	h.session.Start()

	// Wait for session to complete
	select {
	case <-ctx.Done():
		h.logger.Info("context cancelled, closing session")
	case <-h.session.Done():
		h.logger.Info("session completed")
	}
}

// cleanup handles session cleanup.
func (h *liveSessionHandler) cleanup() {
	duration := time.Since(h.startTime)

	// Close session if exists
	if h.session != nil {
		sessionID := h.session.ID()
		h.session.Close()

		// Remove from server map
		h.server.liveSessionsMu.Lock()
		delete(h.server.liveSessions, sessionID)
		h.server.liveSessionsMu.Unlock()
	}

	// Close connection
	if h.conn != nil {
		h.conn.Close()
	}

	// Update metrics
	h.server.sessionCount.Add(-1)
	status := "success"
	if h.session == nil {
		status = "error"
	}
	h.server.metrics.RecordLiveSessionEnd(h.provider, h.model, status, duration)

	h.logger.Info("live session ended",
		"duration_ms", duration.Milliseconds(),
		"status", status,
	)
}

// createRunStreamCreator creates a factory for RunStream instances.
func (h *liveSessionHandler) createRunStreamCreator() live.RunStreamCreator {
	return func(ctx context.Context, config *live.SessionConfig, firstMessage string) (live.RunStreamInterface, error) {
		// Parse model string
		provider, model, err := core.ParseModelString(config.Model)
		if err != nil {
			return nil, err
		}

		// Store for metrics
		h.provider = provider
		h.model = model

		// Create the run stream adapter
		return newLiveRunStreamAdapter(ctx, h.server.engine, config, firstMessage, h.logger)
	}
}

// createSTTFactory creates a factory for STT streams.
func (h *liveSessionHandler) createSTTFactory() live.STTStreamFactory {
	return func(ctx context.Context, config *live.VoiceInputConfig) (live.STTStream, error) {
		if h.server.voicePipeline == nil {
			return nil, fmt.Errorf("voice pipeline not configured")
		}

		// Get STT provider
		sttProvider := h.server.voicePipeline.STTProvider()
		if sttProvider == nil {
			return nil, fmt.Errorf("STT provider not available")
		}

		// Create streaming STT session
		return newSTTStreamAdapter(ctx, sttProvider, config, h.logger)
	}
}

// createLLMFunc creates the LLM function for semantic checks.
func (h *liveSessionHandler) createLLMFunc() live.LLMFunc {
	return func(ctx context.Context, req live.LLMRequest) live.LLMResponse {
		// Use the engine to make the LLM request for semantic checks
		msgReq := &types.MessageRequest{
			Model:     req.Model,
			MaxTokens: 100,
			Messages: []types.Message{
				{Role: "user", Content: req.Prompt},
			},
		}

		resp, err := h.server.engine.CreateMessage(ctx, msgReq)
		if err != nil {
			return live.LLMResponse{Error: err}
		}

		return live.LLMResponse{
			Text: resp.TextContent(),
		}
	}
}

// liveRunStreamAdapter adapts the SDK's RunStream to the live.RunStreamInterface.
type liveRunStreamAdapter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	engine      *core.Engine
	config      *live.SessionConfig
	messages    []types.Message
	events      chan live.RunStreamEvent
	done        chan struct{}
	logger      *slog.Logger
	interrupted bool
}

// newLiveRunStreamAdapter creates a new run stream adapter.
func newLiveRunStreamAdapter(
	ctx context.Context,
	engine *core.Engine,
	config *live.SessionConfig,
	firstMessage string,
	logger *slog.Logger,
) (*liveRunStreamAdapter, error) {
	ctx, cancel := context.WithCancel(ctx)

	adapter := &liveRunStreamAdapter{
		ctx:    ctx,
		cancel: cancel,
		engine: engine,
		config: config,
		messages: []types.Message{
			{Role: "user", Content: firstMessage},
		},
		events: make(chan live.RunStreamEvent, 100),
		done:   make(chan struct{}),
		logger: logger,
	}

	// Start the streaming loop
	go adapter.streamLoop()

	return adapter, nil
}

func (a *liveRunStreamAdapter) Events() <-chan live.RunStreamEvent {
	return a.events
}

func (a *liveRunStreamAdapter) Interrupt(msg live.UserMessage, behavior live.InterruptBehavior) error {
	a.interrupted = true

	// Add the new user message
	a.messages = append(a.messages, types.Message{
		Role:    msg.Role,
		Content: msg.Content,
	})

	// Send interrupt event
	select {
	case a.events <- &live.InterruptedEvent{}:
	default:
	}

	// Restart streaming with new message
	go a.streamLoop()

	return nil
}

func (a *liveRunStreamAdapter) InterruptWithText(text string) error {
	return a.Interrupt(live.UserMessage{Role: "user", Content: text}, live.InterruptDiscard)
}

func (a *liveRunStreamAdapter) Cancel() error {
	a.cancel()
	return nil
}

func (a *liveRunStreamAdapter) Close() error {
	a.cancel()
	close(a.done)
	return nil
}

func (a *liveRunStreamAdapter) streamLoop() {
	a.interrupted = false

	// Build request
	req := &types.MessageRequest{
		Model:    a.config.Model,
		System:   a.config.System,
		Messages: a.messages,
		Stream:   true,
	}

	// Add tools if configured
	if len(a.config.Tools) > 0 {
		for _, tool := range a.config.Tools {
			if t, ok := tool.(types.Tool); ok {
				req.Tools = append(req.Tools, t)
			}
		}
	}

	// Start streaming
	stream, err := a.engine.StreamMessage(a.ctx, req)
	if err != nil {
		a.logger.Error("stream creation failed", "error", err)
		a.emitEvent(&live.RunErrorEvent{Err: err})
		return
	}
	defer stream.Close()

	// Send step start
	a.emitEvent(&live.StepStartEvent{})

	// Track response building
	var responseContent []types.ContentBlock
	var stopReason types.StopReason

	// Process stream events using Next()
	for {
		if a.interrupted {
			return
		}

		select {
		case <-a.ctx.Done():
			return
		default:
		}

		event, err := stream.Next()
		if err != nil {
			// io.EOF means normal end
			if err.Error() != "EOF" {
				a.emitEvent(&live.RunErrorEvent{Err: err})
			}
			break
		}
		if event == nil {
			break
		}

		// Wrap and forward the event
		a.emitEvent(&live.StreamEventWrapper{Evt: event})

		// Track response building
		switch e := event.(type) {
		case types.ContentBlockStartEvent:
			for len(responseContent) <= e.Index {
				responseContent = append(responseContent, nil)
			}
			responseContent[e.Index] = e.ContentBlock
		case types.ContentBlockDeltaEvent:
			// Apply delta
			if e.Index < len(responseContent) {
				responseContent[e.Index] = applyDelta(responseContent[e.Index], e.Delta)
			}
		case types.MessageDeltaEvent:
			stopReason = e.Delta.StopReason
		}
	}

	// Build final response
	if len(responseContent) > 0 {
		// Add assistant message to conversation
		a.messages = append(a.messages, types.Message{
			Role:    "assistant",
			Content: responseContent,
		})

		// Check for tool use
		if stopReason == "tool_use" {
			// Handle tool calls
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

	// Send step complete
	a.emitEvent(&live.StepCompleteEvent{})
}

// applyDelta applies a delta to a content block.
func applyDelta(block types.ContentBlock, delta types.Delta) types.ContentBlock {
	switch d := delta.(type) {
	case types.TextDelta:
		if tb, ok := block.(types.TextBlock); ok {
			tb.Text += d.Text
			return tb
		}
	case types.InputJSONDelta:
		// For tool use blocks, accumulate the partial JSON
		if tu, ok := block.(types.ToolUseBlock); ok {
			_ = tu
			_ = d.PartialJSON
		}
	case types.ThinkingDelta:
		if tb, ok := block.(types.ThinkingBlock); ok {
			tb.Thinking += d.Thinking
			return tb
		}
	}
	return block
}

func (a *liveRunStreamAdapter) emitEvent(event live.RunStreamEvent) {
	select {
	case a.events <- event:
	case <-a.ctx.Done():
	default:
	}
}

// STT stream adapter

type sttStreamAdapter struct {
	ctx        context.Context
	provider   stt.Provider
	config     *live.VoiceInputConfig
	logger     *slog.Logger
	transcript strings.Builder
	lastDelta  string
}

func newSTTStreamAdapter(
	ctx context.Context,
	provider stt.Provider,
	config *live.VoiceInputConfig,
	logger *slog.Logger,
) (*sttStreamAdapter, error) {
	return &sttStreamAdapter{
		ctx:      ctx,
		provider: provider,
		config:   config,
		logger:   logger,
	}, nil
}

func (s *sttStreamAdapter) Write(audio []byte) error {
	// In a real implementation, this would stream audio to the STT provider
	// and receive incremental transcripts back.
	// For now, we'll just accumulate audio and transcribe in batches.

	// TODO: Implement streaming STT when Cartesia streaming API is available
	// For now, this is a placeholder that will be enhanced with actual STT streaming
	return nil
}

func (s *sttStreamAdapter) Transcript() string {
	return s.transcript.String()
}

func (s *sttStreamAdapter) TranscriptDelta() string {
	delta := s.lastDelta
	s.lastDelta = ""
	return delta
}

func (s *sttStreamAdapter) Reset() {
	s.transcript.Reset()
	s.lastDelta = ""
}

func (s *sttStreamAdapter) Close() error {
	return nil
}

// Ensure interfaces are implemented
var _ live.STTStream = (*sttStreamAdapter)(nil)
var _ live.RunStreamInterface = (*liveRunStreamAdapter)(nil)

package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core"
	"github.com/vango-ai/vango/pkg/core/live"
	liveadapters "github.com/vango-ai/vango/pkg/core/live/adapters"
	"github.com/vango-ai/vango/pkg/core/types"
	"github.com/vango-ai/vango/pkg/core/voice/tts"
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
		TTSFactory:       h.createTTSFactory(),
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
		return liveadapters.NewEngineRunStreamAdapter(ctx, liveadapters.EngineRunStreamAdapterConfig{
			Engine:        h.server.engine,
			SessionConfig: config,
			FirstMessage:  firstMessage,
			Logger:        h.logger,
		})
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
		return liveadapters.NewStreamingSTTStream(ctx, liveadapters.StreamingSTTStreamConfig{
			Provider: sttProvider,
			Input:    config,
		})
	}
}

func (h *liveSessionHandler) createTTSFactory() live.TTSFactory {
	return func(ctx context.Context, config *live.VoiceOutputConfig) (*tts.StreamingContext, error) {
		if h.server.voicePipeline == nil {
			return nil, fmt.Errorf("voice pipeline not configured")
		}
		ttsProvider := h.server.voicePipeline.TTSProvider()
		if ttsProvider == nil {
			return nil, fmt.Errorf("TTS provider not available")
		}

		opts := tts.StreamingContextOptions{
			Voice:      config.Voice,
			Speed:      config.Speed,
			Format:     config.Format,
			SampleRate: config.SampleRate,
		}
		return ttsProvider.NewStreamingContext(ctx, opts)
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

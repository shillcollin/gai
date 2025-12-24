# Phase 7: Live Sessions (Real-Time Bidirectional)

**Status:** Not Started
**Priority:** Medium
**Dependencies:** Phase 6 (Additional Providers)

---

## Overview

Phase 7 implements the Live Sessions feature (`/v1/messages/live`) which provides real-time bidirectional communication over WebSocket. This enables:

1. **Real-time voice conversations**: Stream audio in, get audio back immediately
2. **Voice activity detection**: Automatic turn-taking
3. **Interruption handling**: Cancel response mid-stream
4. **Low-latency interactions**: Sub-second response times

Live Sessions can route to:
- **Native realtime models**: OpenAI gpt-4o-realtime, Gemini Live
- **Pipeline mode**: Any model + STT/TTS from Phase 4

---

## Goals

1. Implement WebSocket server for `/v1/messages/live`
2. Implement client-side `Messages.Live()` API
3. Support OpenAI Realtime API (gpt-4o-realtime)
4. Support Gemini Live API
5. Support pipeline mode (STT → Any LLM → TTS)
6. Handle audio streaming, tool calls, and interruptions

---

## Deliverables

### 7.1 Package Structure

```
pkg/core/
├── live/
│   ├── session.go        # Session management
│   ├── events.go         # Event types
│   ├── router.go         # Route to native or pipeline
│   ├── pipeline.go       # STT → LLM → TTS pipeline
│   └── providers/
│       ├── openai.go     # OpenAI Realtime adapter
│       └── gemini.go     # Gemini Live adapter
│
sdk/
├── live.go               # LiveSession client API
└── live_events.go        # Live event types
```

### 7.2 Live Event Types (sdk/live_events.go)

```go
package vango

// === Client Events (Outgoing) ===

// SessionConfigureEvent configures the live session.
type SessionConfigureEvent struct {
    Type       string       `json:"type"` // "session.configure"
    Model      string       `json:"model"`
    Modalities []string     `json:"modalities"` // ["text", "audio"]
    Voice      string       `json:"voice,omitempty"`
    System     string       `json:"system,omitempty"`
    Tools      []Tool       `json:"tools,omitempty"`
    Extensions map[string]any `json:"extensions,omitempty"`
}

// AudioAppendEvent sends audio data.
type AudioAppendEvent struct {
    Type string `json:"type"` // "audio.append"
    Data []byte `json:"-"`    // Raw binary, not JSON
}

// AudioCommitEvent signals end of audio input.
type AudioCommitEvent struct {
    Type string `json:"type"` // "audio.commit"
}

// TextSendEvent sends text input.
type TextSendEvent struct {
    Type string `json:"type"` // "text.send"
    Text string `json:"text"`
}

// ResponseCancelEvent cancels the current response.
type ResponseCancelEvent struct {
    Type string `json:"type"` // "response.cancel"
}

// ToolResultEvent sends a tool result.
type ToolResultSendEvent struct {
    Type      string `json:"type"` // "tool_result.send"
    ToolUseID string `json:"tool_use_id"`
    Content   any    `json:"content"`
}

// === Server Events (Incoming) ===

// LiveEvent is the interface for all live events.
type LiveEvent interface {
    liveEventType() string
}

// SessionCreatedEvent signals session is ready.
type SessionCreatedEvent struct {
    Type      string `json:"type"` // "session.created"
    SessionID string `json:"session_id"`
    Model     string `json:"model"`
}

func (e SessionCreatedEvent) liveEventType() string { return "session.created" }

// InputAudioTranscriptionDelta is a partial transcript of user audio.
type InputAudioTranscriptionDelta struct {
    Type  string `json:"type"` // "input_audio_transcription.delta"
    Delta string `json:"delta"`
}

func (e InputAudioTranscriptionDelta) liveEventType() string { return "input_audio_transcription.delta" }

// InputAudioTranscriptionDone is the final transcript.
type InputAudioTranscriptionDone struct {
    Type string `json:"type"` // "input_audio_transcription.done"
    Text string `json:"text"`
}

func (e InputAudioTranscriptionDone) liveEventType() string { return "input_audio_transcription.done" }

// ResponseAudioDelta is a chunk of response audio.
type ResponseAudioDelta struct {
    Type  string `json:"type"` // "response.audio.delta"
    Delta []byte `json:"delta"` // Raw PCM audio
}

func (e ResponseAudioDelta) liveEventType() string { return "response.audio.delta" }

// ResponseAudioDone signals audio response is complete.
type ResponseAudioDone struct {
    Type string `json:"type"` // "response.audio.done"
}

func (e ResponseAudioDone) liveEventType() string { return "response.audio.done" }

// ResponseTextDelta is a chunk of response text.
type ResponseTextDelta struct {
    Type  string `json:"type"` // "response.text.delta"
    Delta string `json:"delta"`
}

func (e ResponseTextDelta) liveEventType() string { return "response.text.delta" }

// ResponseTextDone signals text response is complete.
type ResponseTextDone struct {
    Type string `json:"type"` // "response.text.done"
    Text string `json:"text"`
}

func (e ResponseTextDone) liveEventType() string { return "response.text.done" }

// ResponseToolCall indicates the model wants to call a tool.
type ResponseToolCall struct {
    Type  string         `json:"type"` // "response.tool_call"
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input"`
}

func (e ResponseToolCall) liveEventType() string { return "response.tool_call" }

// ResponseDone signals the response is complete.
type ResponseDone struct {
    Type  string `json:"type"` // "response.done"
    Usage Usage  `json:"usage"`
}

func (e ResponseDone) liveEventType() string { return "response.done" }

// LiveErrorEvent signals an error.
type LiveErrorEvent struct {
    Type    string `json:"type"` // "error"
    Code    string `json:"code"`
    Message string `json:"message"`
}

func (e LiveErrorEvent) liveEventType() string { return "error" }

// ConversationItemCreated signals a new conversation item.
type ConversationItemCreated struct {
    Type string `json:"type"` // "conversation.item.created"
    Item struct {
        ID      string `json:"id"`
        Type    string `json:"type"` // "message", "function_call", "function_call_output"
        Role    string `json:"role,omitempty"`
        Content []struct {
            Type       string `json:"type"`
            Text       string `json:"text,omitempty"`
            Transcript string `json:"transcript,omitempty"`
        } `json:"content,omitempty"`
    } `json:"item"`
}

func (e ConversationItemCreated) liveEventType() string { return "conversation.item.created" }
```

### 7.3 LiveSession Client (sdk/live.go)

```go
package vango

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "sync/atomic"

    "github.com/gorilla/websocket"
)

// LiveConfig configures a live session.
type LiveConfig struct {
    Model      string            `json:"model"`
    Modalities []string          `json:"modalities,omitempty"` // ["text", "audio"]
    Voice      *VoiceOutputConfig `json:"voice,omitempty"`
    System     string            `json:"system,omitempty"`
    Tools      []Tool            `json:"tools,omitempty"`
    Extensions map[string]any    `json:"extensions,omitempty"`
}

// LiveSession provides real-time bidirectional communication.
type LiveSession struct {
    conn      *websocket.Conn
    events    chan LiveEvent
    done      chan struct{}
    err       error
    closed    atomic.Bool
    mu        sync.Mutex
    sessionID string
}

// Live creates a new live session.
func (s *MessagesService) Live(ctx context.Context, cfg *LiveConfig) (*LiveSession, error) {
    // Determine WebSocket URL
    wsURL := s.client.getLiveURL()

    // Connect
    headers := make(http.Header)
    headers.Set("Authorization", "Bearer "+s.client.apiKey)

    conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
    if err != nil {
        return nil, fmt.Errorf("websocket connect: %w", err)
    }

    session := &LiveSession{
        conn:   conn,
        events: make(chan LiveEvent, 100),
        done:   make(chan struct{}),
    }

    // Start reader
    go session.readLoop()

    // Send configuration
    if err := session.configure(cfg); err != nil {
        conn.Close()
        return nil, err
    }

    return session, nil
}

func (s *LiveSession) configure(cfg *LiveConfig) error {
    configEvent := map[string]any{
        "type":  "session.configure",
        "model": cfg.Model,
    }

    if len(cfg.Modalities) > 0 {
        configEvent["modalities"] = cfg.Modalities
    }
    if cfg.Voice != nil {
        configEvent["voice"] = cfg.Voice.Voice
    }
    if cfg.System != "" {
        configEvent["system"] = cfg.System
    }
    if len(cfg.Tools) > 0 {
        configEvent["tools"] = cfg.Tools
    }

    return s.sendJSON(configEvent)
}

func (s *LiveSession) readLoop() {
    defer close(s.events)
    defer close(s.done)

    for {
        messageType, data, err := s.conn.ReadMessage()
        if err != nil {
            if !s.closed.Load() {
                s.err = err
            }
            return
        }

        switch messageType {
        case websocket.TextMessage:
            event, err := s.parseEvent(data)
            if err != nil {
                continue
            }
            select {
            case s.events <- event:
            case <-s.done:
                return
            }

        case websocket.BinaryMessage:
            // Binary audio data
            event := ResponseAudioDelta{
                Type:  "response.audio.delta",
                Delta: data,
            }
            select {
            case s.events <- event:
            case <-s.done:
                return
            }
        }
    }
}

func (s *LiveSession) parseEvent(data []byte) (LiveEvent, error) {
    var typeObj struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal(data, &typeObj); err != nil {
        return nil, err
    }

    var event LiveEvent
    switch typeObj.Type {
    case "session.created":
        var e SessionCreatedEvent
        json.Unmarshal(data, &e)
        s.sessionID = e.SessionID
        event = e

    case "input_audio_transcription.delta":
        var e InputAudioTranscriptionDelta
        json.Unmarshal(data, &e)
        event = e

    case "input_audio_transcription.done":
        var e InputAudioTranscriptionDone
        json.Unmarshal(data, &e)
        event = e

    case "response.audio.delta":
        var e ResponseAudioDelta
        json.Unmarshal(data, &e)
        event = e

    case "response.text.delta":
        var e ResponseTextDelta
        json.Unmarshal(data, &e)
        event = e

    case "response.text.done":
        var e ResponseTextDone
        json.Unmarshal(data, &e)
        event = e

    case "response.tool_call":
        var e ResponseToolCall
        json.Unmarshal(data, &e)
        event = e

    case "response.done":
        var e ResponseDone
        json.Unmarshal(data, &e)
        event = e

    case "error":
        var e LiveErrorEvent
        json.Unmarshal(data, &e)
        event = e

    case "conversation.item.created":
        var e ConversationItemCreated
        json.Unmarshal(data, &e)
        event = e

    default:
        return nil, fmt.Errorf("unknown event type: %s", typeObj.Type)
    }

    return event, nil
}

// SendAudio sends audio data to the session.
// Audio should be 16-bit PCM at 24kHz mono.
func (s *LiveSession) SendAudio(data []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.closed.Load() {
        return fmt.Errorf("session closed")
    }

    return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

// SendText sends text input to the session.
func (s *LiveSession) SendText(text string) error {
    return s.sendJSON(map[string]any{
        "type": "text.send",
        "text": text,
    })
}

// Commit signals the end of the current input.
func (s *LiveSession) Commit() error {
    return s.sendJSON(map[string]any{
        "type": "audio.commit",
    })
}

// Cancel cancels the current response.
func (s *LiveSession) Cancel() error {
    return s.sendJSON(map[string]any{
        "type": "response.cancel",
    })
}

// SendToolResult sends a tool result back to the session.
func (s *LiveSession) SendToolResult(toolUseID string, content any) error {
    return s.sendJSON(map[string]any{
        "type":        "tool_result.send",
        "tool_use_id": toolUseID,
        "content":     content,
    })
}

func (s *LiveSession) sendJSON(v any) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.closed.Load() {
        return fmt.Errorf("session closed")
    }

    return s.conn.WriteJSON(v)
}

// Events returns the channel of incoming events.
func (s *LiveSession) Events() <-chan LiveEvent {
    return s.events
}

// SessionID returns the session ID.
func (s *LiveSession) SessionID() string {
    return s.sessionID
}

// Err returns any error that occurred.
func (s *LiveSession) Err() error {
    return s.err
}

// Close closes the session.
func (s *LiveSession) Close() error {
    if s.closed.Swap(true) {
        return nil
    }

    // Send close message
    s.conn.WriteMessage(websocket.CloseMessage,
        websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

    return s.conn.Close()
}
```

### 7.4 Live Session Core (pkg/core/live/session.go)

```go
package live

import (
    "context"
    "fmt"
    "sync"

    "github.com/gorilla/websocket"
    "github.com/vango-ai/vango/pkg/core/types"
)

// Session represents a live session.
type Session struct {
    ID         string
    conn       *websocket.Conn
    config     *SessionConfig
    provider   Provider
    mu         sync.Mutex
    done       chan struct{}
    ctx        context.Context
    cancel     context.CancelFunc
}

// SessionConfig holds session configuration.
type SessionConfig struct {
    Model      string
    Modalities []string
    Voice      string
    System     string
    Tools      []types.Tool
    Extensions map[string]any
}

// Provider is the interface for live session backends.
type Provider interface {
    // Connect establishes connection to the backend.
    Connect(ctx context.Context, cfg *SessionConfig) error

    // SendAudio sends audio to the backend.
    SendAudio(data []byte) error

    // SendText sends text to the backend.
    SendText(text string) error

    // Commit signals end of input.
    Commit() error

    // Cancel cancels current response.
    Cancel() error

    // SendToolResult sends tool result.
    SendToolResult(toolUseID string, content any) error

    // Events returns channel of events from backend.
    Events() <-chan Event

    // Close closes the connection.
    Close() error
}

// NewSession creates a new live session.
func NewSession(conn *websocket.Conn) *Session {
    ctx, cancel := context.WithCancel(context.Background())
    return &Session{
        ID:     generateSessionID(),
        conn:   conn,
        done:   make(chan struct{}),
        ctx:    ctx,
        cancel: cancel,
    }
}

// Configure sets up the session.
func (s *Session) Configure(cfg *SessionConfig) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.config = cfg

    // Determine provider based on model
    provider, err := s.createProvider(cfg.Model)
    if err != nil {
        return err
    }

    s.provider = provider

    // Connect to backend
    if err := provider.Connect(s.ctx, cfg); err != nil {
        return err
    }

    // Start forwarding events
    go s.forwardEvents()

    return nil
}

func (s *Session) createProvider(model string) (Provider, error) {
    // Parse model to determine provider
    provider, _ := parseModel(model)

    switch provider {
    case "openai":
        if isRealtimeModel(model) {
            return NewOpenAIRealtimeProvider(), nil
        }
        return NewPipelineProvider(model), nil

    case "gemini":
        if isLiveModel(model) {
            return NewGeminiLiveProvider(), nil
        }
        return NewPipelineProvider(model), nil

    default:
        // Fall back to pipeline mode
        return NewPipelineProvider(model), nil
    }
}

func (s *Session) forwardEvents() {
    for {
        select {
        case event, ok := <-s.provider.Events():
            if !ok {
                return
            }
            s.sendEvent(event)
        case <-s.done:
            return
        }
    }
}

func (s *Session) sendEvent(event Event) {
    s.mu.Lock()
    defer s.mu.Unlock()

    switch e := event.(type) {
    case *AudioDeltaEvent:
        // Send as binary
        s.conn.WriteMessage(websocket.BinaryMessage, e.Data)
    default:
        // Send as JSON
        s.conn.WriteJSON(event)
    }
}

// HandleMessage processes an incoming message.
func (s *Session) HandleMessage(msgType int, data []byte) error {
    if s.provider == nil {
        return fmt.Errorf("session not configured")
    }

    switch msgType {
    case websocket.TextMessage:
        return s.handleTextMessage(data)
    case websocket.BinaryMessage:
        return s.provider.SendAudio(data)
    default:
        return nil
    }
}

func (s *Session) handleTextMessage(data []byte) error {
    var msg struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal(data, &msg); err != nil {
        return err
    }

    switch msg.Type {
    case "text.send":
        var textMsg struct {
            Text string `json:"text"`
        }
        json.Unmarshal(data, &textMsg)
        return s.provider.SendText(textMsg.Text)

    case "audio.commit":
        return s.provider.Commit()

    case "response.cancel":
        return s.provider.Cancel()

    case "tool_result.send":
        var toolMsg struct {
            ToolUseID string `json:"tool_use_id"`
            Content   any    `json:"content"`
        }
        json.Unmarshal(data, &toolMsg)
        return s.provider.SendToolResult(toolMsg.ToolUseID, toolMsg.Content)

    default:
        return fmt.Errorf("unknown message type: %s", msg.Type)
    }
}

// Close closes the session.
func (s *Session) Close() {
    s.cancel()
    close(s.done)
    if s.provider != nil {
        s.provider.Close()
    }
}

func isRealtimeModel(model string) bool {
    return strings.Contains(model, "realtime")
}

func isLiveModel(model string) bool {
    return strings.Contains(model, "live")
}

func generateSessionID() string {
    return "sess_" + randomString(16)
}
```

### 7.5 OpenAI Realtime Provider (pkg/core/live/providers/openai.go)

```go
package providers

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/gorilla/websocket"
    "github.com/vango-ai/vango/pkg/core/live"
)

const openaiRealtimeURL = "wss://api.openai.com/v1/realtime"

// OpenAIRealtimeProvider connects to OpenAI Realtime API.
type OpenAIRealtimeProvider struct {
    apiKey string
    conn   *websocket.Conn
    events chan live.Event
    done   chan struct{}
}

func NewOpenAIRealtimeProvider(apiKey string) *OpenAIRealtimeProvider {
    return &OpenAIRealtimeProvider{
        apiKey: apiKey,
        events: make(chan live.Event, 100),
        done:   make(chan struct{}),
    }
}

func (p *OpenAIRealtimeProvider) Connect(ctx context.Context, cfg *live.SessionConfig) error {
    // Connect to OpenAI
    headers := make(http.Header)
    headers.Set("Authorization", "Bearer "+p.apiKey)
    headers.Set("OpenAI-Beta", "realtime=v1")

    model := stripProviderPrefix(cfg.Model)
    url := fmt.Sprintf("%s?model=%s", openaiRealtimeURL, model)

    conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, headers)
    if err != nil {
        return err
    }
    p.conn = conn

    // Send session update
    sessionUpdate := map[string]any{
        "type": "session.update",
        "session": map[string]any{
            "modalities":                  cfg.Modalities,
            "voice":                       cfg.Voice,
            "instructions":                cfg.System,
            "input_audio_transcription":   map[string]any{"model": "whisper-1"},
        },
    }

    if len(cfg.Tools) > 0 {
        tools := make([]map[string]any, len(cfg.Tools))
        for i, tool := range cfg.Tools {
            tools[i] = map[string]any{
                "type":        "function",
                "name":        tool.Name,
                "description": tool.Description,
                "parameters":  tool.InputSchema,
            }
        }
        sessionUpdate["session"].(map[string]any)["tools"] = tools
    }

    if err := conn.WriteJSON(sessionUpdate); err != nil {
        return err
    }

    // Start reader
    go p.readLoop()

    return nil
}

func (p *OpenAIRealtimeProvider) readLoop() {
    defer close(p.events)

    for {
        _, data, err := p.conn.ReadMessage()
        if err != nil {
            return
        }

        event := p.translateEvent(data)
        if event != nil {
            select {
            case p.events <- event:
            case <-p.done:
                return
            }
        }
    }
}

func (p *OpenAIRealtimeProvider) translateEvent(data []byte) live.Event {
    var typeObj struct {
        Type string `json:"type"`
    }
    json.Unmarshal(data, &typeObj)

    switch typeObj.Type {
    case "session.created":
        var e struct {
            Session struct {
                ID string `json:"id"`
            } `json:"session"`
        }
        json.Unmarshal(data, &e)
        return &live.SessionCreatedEvent{
            Type:      "session.created",
            SessionID: e.Session.ID,
        }

    case "conversation.item.input_audio_transcription.completed":
        var e struct {
            Transcript string `json:"transcript"`
        }
        json.Unmarshal(data, &e)
        return &live.InputAudioTranscriptionDone{
            Type: "input_audio_transcription.done",
            Text: e.Transcript,
        }

    case "response.audio.delta":
        var e struct {
            Delta string `json:"delta"` // base64
        }
        json.Unmarshal(data, &e)
        audioData, _ := base64.StdEncoding.DecodeString(e.Delta)
        return &live.ResponseAudioDelta{
            Type:  "response.audio.delta",
            Delta: audioData,
        }

    case "response.audio_transcript.delta":
        var e struct {
            Delta string `json:"delta"`
        }
        json.Unmarshal(data, &e)
        return &live.ResponseTextDelta{
            Type:  "response.text.delta",
            Delta: e.Delta,
        }

    case "response.function_call_arguments.done":
        var e struct {
            CallID    string `json:"call_id"`
            Name      string `json:"name"`
            Arguments string `json:"arguments"`
        }
        json.Unmarshal(data, &e)
        var input map[string]any
        json.Unmarshal([]byte(e.Arguments), &input)
        return &live.ResponseToolCall{
            Type:  "response.tool_call",
            ID:    e.CallID,
            Name:  e.Name,
            Input: input,
        }

    case "response.done":
        var e struct {
            Response struct {
                Usage struct {
                    InputTokens  int `json:"input_tokens"`
                    OutputTokens int `json:"output_tokens"`
                } `json:"usage"`
            } `json:"response"`
        }
        json.Unmarshal(data, &e)
        return &live.ResponseDone{
            Type: "response.done",
            Usage: types.Usage{
                InputTokens:  e.Response.Usage.InputTokens,
                OutputTokens: e.Response.Usage.OutputTokens,
            },
        }

    case "error":
        var e struct {
            Error struct {
                Code    string `json:"code"`
                Message string `json:"message"`
            } `json:"error"`
        }
        json.Unmarshal(data, &e)
        return &live.ErrorEvent{
            Type:    "error",
            Code:    e.Error.Code,
            Message: e.Error.Message,
        }
    }

    return nil
}

func (p *OpenAIRealtimeProvider) SendAudio(data []byte) error {
    // OpenAI expects base64 encoded audio
    encoded := base64.StdEncoding.EncodeToString(data)
    return p.conn.WriteJSON(map[string]any{
        "type":  "input_audio_buffer.append",
        "audio": encoded,
    })
}

func (p *OpenAIRealtimeProvider) SendText(text string) error {
    return p.conn.WriteJSON(map[string]any{
        "type": "conversation.item.create",
        "item": map[string]any{
            "type": "message",
            "role": "user",
            "content": []map[string]any{
                {"type": "input_text", "text": text},
            },
        },
    })
}

func (p *OpenAIRealtimeProvider) Commit() error {
    if err := p.conn.WriteJSON(map[string]any{
        "type": "input_audio_buffer.commit",
    }); err != nil {
        return err
    }
    return p.conn.WriteJSON(map[string]any{
        "type": "response.create",
    })
}

func (p *OpenAIRealtimeProvider) Cancel() error {
    return p.conn.WriteJSON(map[string]any{
        "type": "response.cancel",
    })
}

func (p *OpenAIRealtimeProvider) SendToolResult(toolUseID string, content any) error {
    contentStr, _ := json.Marshal(content)
    if err := p.conn.WriteJSON(map[string]any{
        "type": "conversation.item.create",
        "item": map[string]any{
            "type":    "function_call_output",
            "call_id": toolUseID,
            "output":  string(contentStr),
        },
    }); err != nil {
        return err
    }
    return p.conn.WriteJSON(map[string]any{
        "type": "response.create",
    })
}

func (p *OpenAIRealtimeProvider) Events() <-chan live.Event {
    return p.events
}

func (p *OpenAIRealtimeProvider) Close() error {
    close(p.done)
    return p.conn.Close()
}
```

### 7.6 Pipeline Provider (pkg/core/live/providers/pipeline.go)

```go
package providers

import (
    "context"
    "sync"

    "github.com/vango-ai/vango/pkg/core"
    "github.com/vango-ai/vango/pkg/core/live"
    "github.com/vango-ai/vango/pkg/core/voice"
    "github.com/vango-ai/vango/pkg/core/voice/stt"
    "github.com/vango-ai/vango/pkg/core/voice/tts"
)

// PipelineProvider uses STT → LLM → TTS for any model.
type PipelineProvider struct {
    model         string
    llmProvider   core.Provider
    sttProvider   stt.Provider
    ttsProvider   tts.Provider
    events        chan live.Event
    done          chan struct{}
    audioBuffer   []byte
    mu            sync.Mutex
    config        *live.SessionConfig
    conversation  []types.Message
}

func NewPipelineProvider(model string, llm core.Provider, stt stt.Provider, tts tts.Provider) *PipelineProvider {
    return &PipelineProvider{
        model:       model,
        llmProvider: llm,
        sttProvider: stt,
        ttsProvider: tts,
        events:      make(chan live.Event, 100),
        done:        make(chan struct{}),
    }
}

func (p *PipelineProvider) Connect(ctx context.Context, cfg *live.SessionConfig) error {
    p.config = cfg

    // Initialize conversation with system message
    if cfg.System != "" {
        // System is handled separately in LLM calls
    }

    // Signal session created
    p.events <- &live.SessionCreatedEvent{
        Type:      "session.created",
        SessionID: "pipe_" + randomString(16),
        Model:     p.model,
    }

    return nil
}

func (p *PipelineProvider) SendAudio(data []byte) error {
    p.mu.Lock()
    p.audioBuffer = append(p.audioBuffer, data...)
    p.mu.Unlock()
    return nil
}

func (p *PipelineProvider) SendText(text string) error {
    // Add user message
    p.mu.Lock()
    p.conversation = append(p.conversation, types.Message{
        Role:    "user",
        Content: text,
    })
    p.mu.Unlock()

    // Process
    go p.processConversation(context.Background())
    return nil
}

func (p *PipelineProvider) Commit() error {
    p.mu.Lock()
    audioData := p.audioBuffer
    p.audioBuffer = nil
    p.mu.Unlock()

    if len(audioData) == 0 {
        return nil
    }

    // Transcribe audio
    ctx := context.Background()
    transcript, err := p.sttProvider.Transcribe(ctx, bytes.NewReader(audioData), stt.TranscribeOptions{})
    if err != nil {
        p.events <- &live.ErrorEvent{
            Type:    "error",
            Code:    "transcription_failed",
            Message: err.Error(),
        }
        return err
    }

    // Send transcript event
    p.events <- &live.InputAudioTranscriptionDone{
        Type: "input_audio_transcription.done",
        Text: transcript.Text,
    }

    // Add to conversation
    p.mu.Lock()
    p.conversation = append(p.conversation, types.Message{
        Role:    "user",
        Content: transcript.Text,
    })
    p.mu.Unlock()

    // Process conversation
    go p.processConversation(ctx)
    return nil
}

func (p *PipelineProvider) processConversation(ctx context.Context) {
    p.mu.Lock()
    messages := make([]types.Message, len(p.conversation))
    copy(messages, p.conversation)
    p.mu.Unlock()

    // Call LLM with streaming
    req := &types.MessageRequest{
        Model:    p.model,
        Messages: messages,
        System:   p.config.System,
        Tools:    p.config.Tools,
        Stream:   true,
    }

    stream, err := p.llmProvider.StreamMessage(ctx, req)
    if err != nil {
        p.events <- &live.ErrorEvent{
            Type:    "error",
            Code:    "llm_error",
            Message: err.Error(),
        }
        return
    }
    defer stream.Close()

    // Set up TTS synthesizer
    synthesizer := voice.NewStreamingSynthesizer(p.ttsProvider, p.config.Voice)
    go func() {
        for chunk := range synthesizer.Chunks() {
            p.events <- &live.ResponseAudioDelta{
                Type:  "response.audio.delta",
                Delta: chunk.Data,
            }
        }
    }()

    // Process LLM stream
    var responseText strings.Builder
    var toolCalls []types.ToolUseBlock

    for {
        event, err := stream.Next()
        if err != nil {
            if err == io.EOF {
                break
            }
            return
        }

        switch e := event.(type) {
        case types.ContentBlockDeltaEvent:
            if delta, ok := e.Delta.(types.TextDelta); ok {
                responseText.WriteString(delta.Text)

                // Send text delta
                p.events <- &live.ResponseTextDelta{
                    Type:  "response.text.delta",
                    Delta: delta.Text,
                }

                // Feed to TTS
                synthesizer.AddText(ctx, delta.Text)
            }

        case types.ContentBlockStartEvent:
            if tu, ok := e.ContentBlock.(types.ToolUseBlock); ok {
                toolCalls = append(toolCalls, tu)
            }

        case types.MessageDeltaEvent:
            // Handle tool calls
            for _, tc := range toolCalls {
                p.events <- &live.ResponseToolCall{
                    Type:  "response.tool_call",
                    ID:    tc.ID,
                    Name:  tc.Name,
                    Input: tc.Input,
                }
            }
        }
    }

    // Flush TTS
    synthesizer.Flush(ctx)
    synthesizer.Close()

    // Add assistant response to conversation
    if responseText.Len() > 0 {
        p.mu.Lock()
        p.conversation = append(p.conversation, types.Message{
            Role:    "assistant",
            Content: responseText.String(),
        })
        p.mu.Unlock()
    }

    // Send done event
    p.events <- &live.ResponseDone{
        Type: "response.done",
    }
}

func (p *PipelineProvider) Cancel() error {
    // Cancel current processing (would need context cancellation)
    return nil
}

func (p *PipelineProvider) SendToolResult(toolUseID string, content any) error {
    // Add tool result to conversation
    contentStr, _ := json.Marshal(content)

    p.mu.Lock()
    p.conversation = append(p.conversation, types.Message{
        Role: "user",
        Content: []types.ContentBlock{
            types.ToolResultBlock{
                Type:      "tool_result",
                ToolUseID: toolUseID,
                Content: []types.ContentBlock{
                    types.TextBlock{Type: "text", Text: string(contentStr)},
                },
            },
        },
    })
    p.mu.Unlock()

    // Continue conversation
    go p.processConversation(context.Background())
    return nil
}

func (p *PipelineProvider) Events() <-chan live.Event {
    return p.events
}

func (p *PipelineProvider) Close() error {
    close(p.done)
    return nil
}
```

---

## Testing Strategy

```go
// +build integration

func TestLive_OpenAIRealtime(t *testing.T) {
    if os.Getenv("OPENAI_API_KEY") == "" {
        t.Skip("OPENAI_API_KEY not set")
    }

    client := vango.NewClient()

    session, err := client.Messages.Live(testCtx, &vango.LiveConfig{
        Model:      "openai/gpt-4o-realtime-preview",
        Modalities: []string{"text", "audio"},
        Voice:      &vango.VoiceOutputConfig{Voice: "alloy"},
    })
    require.NoError(t, err)
    defer session.Close()

    // Wait for session created
    var sessionCreated bool
    for event := range session.Events() {
        if _, ok := event.(vango.SessionCreatedEvent); ok {
            sessionCreated = true
            break
        }
    }
    assert.True(t, sessionCreated)

    // Send text
    err = session.SendText("Say hello in one word.")
    require.NoError(t, err)

    // Wait for response
    var gotResponse bool
    for event := range session.Events() {
        if _, ok := event.(vango.ResponseDone); ok {
            gotResponse = true
            break
        }
    }
    assert.True(t, gotResponse)
}

func TestLive_PipelineMode(t *testing.T) {
    requireAllVoiceKeys(t)

    client := vango.NewClient()

    session, err := client.Messages.Live(testCtx, &vango.LiveConfig{
        Model:      "anthropic/claude-sonnet-4", // Non-realtime model
        Modalities: []string{"text", "audio"},
        Voice:      &vango.VoiceOutputConfig{Provider: "elevenlabs", Voice: "rachel"},
    })
    require.NoError(t, err)
    defer session.Close()

    // Send audio
    audioData := fixtures.Audio("hello.wav")

    // Send in chunks
    chunkSize := 4096
    for i := 0; i < len(audioData); i += chunkSize {
        end := i + chunkSize
        if end > len(audioData) {
            end = len(audioData)
        }
        session.SendAudio(audioData[i:end])
    }

    // Commit audio
    session.Commit()

    // Wait for transcript and response
    var gotTranscript, gotResponse bool
    for event := range session.Events() {
        switch event.(type) {
        case vango.InputAudioTranscriptionDone:
            gotTranscript = true
        case vango.ResponseDone:
            gotResponse = true
            break
        }
    }

    assert.True(t, gotTranscript)
    assert.True(t, gotResponse)
}
```

---

## Acceptance Criteria

1. [ ] WebSocket connection established successfully
2. [ ] Session configuration works
3. [ ] Audio streaming works (send and receive)
4. [ ] Text messaging works
5. [ ] Tool calls work in live sessions
6. [ ] Interruption (cancel) works
7. [ ] OpenAI Realtime integration works
8. [ ] Pipeline mode works with any model
9. [ ] Proper error handling and cleanup

---

## Files to Create

```
pkg/core/live/session.go
pkg/core/live/events.go
pkg/core/live/router.go
pkg/core/live/providers/openai.go
pkg/core/live/providers/gemini.go
pkg/core/live/providers/pipeline.go
sdk/live.go
sdk/live_events.go
tests/integration/live_test.go
```

---

## Estimated Effort

- Core live session: ~400 lines
- OpenAI Realtime adapter: ~300 lines
- Pipeline provider: ~300 lines
- SDK client: ~250 lines
- Tests: ~300 lines
- **Total: ~1550 lines**

---

## Next Phase

Phase 8: Proxy Server - implements the HTTP server that wraps pkg/core for non-Go clients.

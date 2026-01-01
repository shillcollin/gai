package vango

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vango-ai/vango/pkg/core/live"
	liveadapters "github.com/vango-ai/vango/pkg/core/live/adapters"
	"github.com/vango-ai/vango/pkg/core/types"
	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

// LiveConfig configures a real-time bidirectional session.
type LiveConfig struct {
	// Model is the LLM model to use for the session.
	Model string `json:"model"`

	// System is the system prompt for the session.
	System string `json:"system,omitempty"`

	// Tools are the tools available to the model.
	Tools []types.Tool `json:"tools,omitempty"`

	// Voice configures voice input/output settings.
	Voice *LiveVoiceConfig `json:"voice,omitempty"`
}

// LiveVoiceConfig contains voice settings for live sessions.
type LiveVoiceConfig struct {
	// Input configures speech-to-text.
	Input *LiveVoiceInputConfig `json:"input,omitempty"`

	// Output configures text-to-speech.
	Output *LiveVoiceOutputConfig `json:"output,omitempty"`

	// VAD configures voice activity detection.
	VAD *LiveVADConfig `json:"vad,omitempty"`

	// Interrupt configures barge-in detection.
	Interrupt *LiveInterruptConfig `json:"interrupt,omitempty"`
}

// LiveVoiceInputConfig configures speech-to-text.
type LiveVoiceInputConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Language string `json:"language,omitempty"`
}

// LiveVoiceOutputConfig configures text-to-speech.
type LiveVoiceOutputConfig struct {
	Provider   string  `json:"provider,omitempty"`
	Voice      string  `json:"voice"`
	Speed      float64 `json:"speed,omitempty"`
	Format     string  `json:"format,omitempty"`
	SampleRate int     `json:"sample_rate,omitempty"` // Sample rate in Hz (default: 24000)
}

// LiveVADConfig controls voice activity detection.
type LiveVADConfig struct {
	// Model is the LLM for semantic turn completion (default: haiku).
	Model string `json:"model,omitempty"`

	// EnergyThreshold is the RMS threshold for silence (default: 0.02).
	EnergyThreshold float64 `json:"energy_threshold,omitempty"`

	// SilenceDurationMs is silence duration before semantic check (default: 600).
	SilenceDurationMs int `json:"silence_duration_ms,omitempty"`

	// SemanticCheck enables semantic turn completion (default: true).
	SemanticCheck *bool `json:"semantic_check,omitempty"`

	// MinWordsForCheck is minimum words before semantic check (default: 2).
	MinWordsForCheck int `json:"min_words_for_check,omitempty"`

	// MaxSilenceMs is the force commit timeout (default: 3000).
	MaxSilenceMs int `json:"max_silence_ms,omitempty"`
}

// LiveInterruptMode specifies how interrupts are detected.
type LiveInterruptMode string

const (
	LiveInterruptModeAuto     LiveInterruptMode = "auto"
	LiveInterruptModeManual   LiveInterruptMode = "manual"
	LiveInterruptModeDisabled LiveInterruptMode = "disabled"
)

// LiveSaveBehavior specifies how to handle partial responses when interrupted.
type LiveSaveBehavior string

const (
	LiveSaveBehaviorDiscard LiveSaveBehavior = "discard"
	LiveSaveBehaviorSave    LiveSaveBehavior = "save"
	LiveSaveBehaviorMarked  LiveSaveBehavior = "marked"
)

// LiveInterruptConfig controls barge-in detection.
type LiveInterruptConfig struct {
	// Mode is the interrupt detection mode (default: "auto").
	Mode LiveInterruptMode `json:"mode,omitempty"`

	// EnergyThreshold for detecting potential interrupt (default: 0.05).
	EnergyThreshold float64 `json:"energy_threshold,omitempty"`

	// DebounceMs is minimum sustained speech before check (default: 100).
	DebounceMs int `json:"debounce_ms,omitempty"`

	// SemanticCheck distinguishes interrupts from backchannels (default: true).
	SemanticCheck *bool `json:"semantic_check,omitempty"`

	// SemanticModel is the LLM for interrupt detection.
	SemanticModel string `json:"semantic_model,omitempty"`

	// SavePartial specifies how to handle partial response (default: "marked").
	SavePartial LiveSaveBehavior `json:"save_partial,omitempty"`
}

// LiveStream represents a real-time bidirectional voice/text session.
// It manages a WebSocket connection to the server and provides methods
// for sending audio, text, and receiving events.
type LiveStream struct {
	config *LiveConfig
	conn   *websocket.Conn

	// Session state
	sessionID string
	state     liveStreamState
	stateMu   sync.RWMutex

	// Event channels
	events      chan LiveEvent
	audioOut    chan []byte // Outgoing audio from TTS
	transcripts chan LiveTranscriptEvent

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool

	// WebSocket write mutex
	writeMu sync.Mutex

	// Tool handlers for automatic tool execution
	toolHandlers   map[string]ToolHandler
	toolHandlersMu sync.RWMutex

	// Direct mode adapter (nil for proxy mode)
	directAdapter *directSessionAdapter

	// Callbacks
	onError      func(error)
	onConnect    func(sessionID string)
	onDisconnect func()
}

type liveStreamState int

const (
	liveStateConnecting liveStreamState = iota
	liveStateConfiguring
	liveStateReady
	liveStateClosed
)

// LiveEvent is an event from the live session.
type LiveEvent interface {
	liveEventType() string
}

// LiveSessionCreatedEvent signals the session was created.
type LiveSessionCreatedEvent struct {
	SessionID  string `json:"session_id"`
	Model      string `json:"model"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

func (e LiveSessionCreatedEvent) liveEventType() string { return "session.created" }

// LiveVADEvent signals VAD state changes.
type LiveVADEvent struct {
	State      string `json:"state"` // "listening", "analyzing", "silence"
	DurationMs int    `json:"duration_ms,omitempty"`
}

func (e LiveVADEvent) liveEventType() string { return "vad" }

// LiveAudioEvent contains audio data from the session.
type LiveAudioEvent struct {
	Data       []byte `json:"data"`
	Format     string `json:"format"`      // "pcm" for raw PCM, "wav" for WAV
	SampleRate int    `json:"sample_rate"` // Sample rate in Hz (e.g., 44100)
	Channels   int    `json:"channels"`    // Number of channels (1=mono, 2=stereo)
}

func (e LiveAudioEvent) liveEventType() string { return "audio" }

// LiveTranscriptEvent contains transcript text.
type LiveTranscriptEvent struct {
	Role    string `json:"role"` // "user" or "assistant"
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

func (e LiveTranscriptEvent) liveEventType() string { return "transcript" }

// LiveTextDeltaEvent contains streaming text from the model.
type LiveTextDeltaEvent struct {
	Text string `json:"text"`
}

func (e LiveTextDeltaEvent) liveEventType() string { return "text_delta" }

// LiveInputCommittedEvent signals the user's turn was committed.
type LiveInputCommittedEvent struct {
	Transcript string `json:"transcript"`
}

func (e LiveInputCommittedEvent) liveEventType() string { return "input.committed" }

// LiveToolCallEvent signals a tool call from the model.
type LiveToolCallEvent struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (e LiveToolCallEvent) liveEventType() string { return "tool_call" }

// LiveInterruptEvent signals an interrupt state change.
type LiveInterruptEvent struct {
	State           string `json:"state"` // "detecting", "dismissed", "confirmed"
	Transcript      string `json:"transcript,omitempty"`
	PartialText     string `json:"partial_text,omitempty"`
	Reason          string `json:"reason,omitempty"` // for dismissed: "backchannel", etc.
	AudioPositionMs int    `json:"audio_position_ms,omitempty"`
}

func (e LiveInterruptEvent) liveEventType() string { return "interrupt" }

// LiveMessageStopEvent signals the model finished responding.
type LiveMessageStopEvent struct {
	StopReason string `json:"stop_reason"`
}

func (e LiveMessageStopEvent) liveEventType() string { return "message.stop" }

// LiveErrorEvent signals an error.
type LiveErrorEvent struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

func (e LiveErrorEvent) liveEventType() string { return "error" }

// LiveContentBlockEvent signals content block lifecycle events.
type LiveContentBlockEvent struct {
	Event        string `json:"event"` // "start", "delta", "stop"
	Index        int    `json:"index"`
	ContentBlock any    `json:"content_block,omitempty"`
	Delta        any    `json:"delta,omitempty"`
}

func (e LiveContentBlockEvent) liveEventType() string { return "content_block" }

// LiveStreamOption configures a LiveStream.
type LiveStreamOption func(*LiveStream)

// WithLiveToolHandler registers a tool handler for automatic execution.
func WithLiveToolHandler(name string, handler ToolHandler) LiveStreamOption {
	return func(ls *LiveStream) {
		ls.toolHandlersMu.Lock()
		ls.toolHandlers[name] = handler
		ls.toolHandlersMu.Unlock()
	}
}

// WithLiveToolHandlers registers multiple tool handlers.
func WithLiveToolHandlers(handlers map[string]ToolHandler) LiveStreamOption {
	return func(ls *LiveStream) {
		ls.toolHandlersMu.Lock()
		for name, handler := range handlers {
			ls.toolHandlers[name] = handler
		}
		ls.toolHandlersMu.Unlock()
	}
}

// WithLiveOnError sets an error callback.
func WithLiveOnError(fn func(error)) LiveStreamOption {
	return func(ls *LiveStream) {
		ls.onError = fn
	}
}

// WithLiveOnConnect sets a connection callback.
func WithLiveOnConnect(fn func(sessionID string)) LiveStreamOption {
	return func(ls *LiveStream) {
		ls.onConnect = fn
	}
}

// WithLiveOnDisconnect sets a disconnection callback.
func WithLiveOnDisconnect(fn func()) LiveStreamOption {
	return func(ls *LiveStream) {
		ls.onDisconnect = fn
	}
}

// newLiveStream creates a new LiveStream connected to the given WebSocket URL.
func newLiveStream(ctx context.Context, wsURL string, cfg *LiveConfig, opts ...LiveStreamOption) (*LiveStream, error) {
	ctx, cancel := context.WithCancel(ctx)

	ls := &LiveStream{
		config:       cfg,
		events:       make(chan LiveEvent, 100),
		audioOut:     make(chan []byte, 100),
		transcripts:  make(chan LiveTranscriptEvent, 100),
		done:         make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		toolHandlers: make(map[string]ToolHandler),
		state:        liveStateConnecting,
	}

	for _, opt := range opts {
		opt(ls)
	}

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	ls.conn = conn

	// Start read/write loops
	go ls.readLoop()

	// Send configuration
	if err := ls.sendConfig(); err != nil {
		ls.Close()
		return nil, fmt.Errorf("send config: %w", err)
	}

	return ls, nil
}

// sendConfig sends the session configuration message.
func (ls *LiveStream) sendConfig() error {
	ls.stateMu.Lock()
	ls.state = liveStateConfiguring
	ls.stateMu.Unlock()

	msg := live.SessionConfigureMessage{
		Type: live.EventTypeSessionConfigure,
		Config: &live.SessionConfig{
			Model:  ls.config.Model,
			System: ls.config.System,
		},
	}

	// Convert tools
	if len(ls.config.Tools) > 0 {
		msg.Config.Tools = make([]any, len(ls.config.Tools))
		for i, t := range ls.config.Tools {
			msg.Config.Tools[i] = t
		}
	}

	// Convert voice config
	if ls.config.Voice != nil {
		msg.Config.Voice = convertVoiceConfig(ls.config.Voice)
	}

	return ls.sendJSON(msg)
}

func convertVoiceConfig(vc *LiveVoiceConfig) *live.VoiceConfig {
	if vc == nil {
		return nil
	}

	result := &live.VoiceConfig{}

	if vc.Input != nil {
		result.Input = &live.VoiceInputConfig{
			Provider: vc.Input.Provider,
			Model:    vc.Input.Model,
			Language: vc.Input.Language,
		}
	}

	if vc.Output != nil {
		result.Output = &live.VoiceOutputConfig{
			Provider:   vc.Output.Provider,
			Voice:      vc.Output.Voice,
			Speed:      vc.Output.Speed,
			Format:     vc.Output.Format,
			SampleRate: vc.Output.SampleRate,
		}
	}

	if vc.VAD != nil {
		result.VAD = &live.VADConfig{
			Model:             vc.VAD.Model,
			EnergyThreshold:   vc.VAD.EnergyThreshold,
			SilenceDurationMs: vc.VAD.SilenceDurationMs,
			SemanticCheck:     vc.VAD.SemanticCheck,
			MinWordsForCheck:  vc.VAD.MinWordsForCheck,
			MaxSilenceMs:      vc.VAD.MaxSilenceMs,
		}
	}

	if vc.Interrupt != nil {
		result.Interrupt = &live.InterruptConfig{
			Mode:            live.InterruptMode(vc.Interrupt.Mode),
			EnergyThreshold: vc.Interrupt.EnergyThreshold,
			DebounceMs:      vc.Interrupt.DebounceMs,
			SemanticCheck:   vc.Interrupt.SemanticCheck,
			SemanticModel:   vc.Interrupt.SemanticModel,
			SavePartial:     live.SaveBehavior(vc.Interrupt.SavePartial),
		}
	}

	return result
}

// readLoop reads messages from the WebSocket.
func (ls *LiveStream) readLoop() {
	defer func() {
		ls.closeOnce.Do(func() {
			close(ls.done)
			close(ls.events)
			close(ls.audioOut)
			close(ls.transcripts)
		})
		if ls.onDisconnect != nil {
			ls.onDisconnect()
		}
	}()

	for {
		select {
		case <-ls.ctx.Done():
			return
		default:
		}

		messageType, data, err := ls.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				ls.emitError("read_error", err.Error())
			}
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			// Audio data from TTS
			sampleRate := 44100 // Default sample rate
			if ls.config != nil && ls.config.Voice != nil && ls.config.Voice.Output != nil && ls.config.Voice.Output.SampleRate > 0 {
				sampleRate = ls.config.Voice.Output.SampleRate
			}
			ls.emitEvent(LiveAudioEvent{Data: data, Format: "pcm", SampleRate: sampleRate, Channels: 1})
			select {
			case ls.audioOut <- data:
			default:
				// Drop if buffer full
			}

		case websocket.TextMessage:
			ls.handleServerMessage(data)
		}
	}
}

func (ls *LiveStream) handleServerMessage(data []byte) {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		ls.emitError("parse_error", "invalid JSON: "+err.Error())
		return
	}

	switch msg.Type {
	case live.EventTypeSessionCreated:
		var created live.SessionCreatedMessage
		if err := json.Unmarshal(data, &created); err != nil {
			ls.emitError("parse_error", err.Error())
			return
		}
		ls.sessionID = created.SessionID
		ls.stateMu.Lock()
		ls.state = liveStateReady
		ls.stateMu.Unlock()
		ls.emitEvent(LiveSessionCreatedEvent{
			SessionID:  created.SessionID,
			Model:      created.Config.Model,
			SampleRate: created.Config.SampleRate,
			Channels:   created.Config.Channels,
		})
		if ls.onConnect != nil {
			ls.onConnect(created.SessionID)
		}

	case live.EventTypeVADListening:
		ls.emitEvent(LiveVADEvent{State: "listening"})

	case live.EventTypeVADAnalyzing:
		ls.emitEvent(LiveVADEvent{State: "analyzing"})

	case live.EventTypeVADSilence:
		var silence live.VADSilenceMessage
		json.Unmarshal(data, &silence)
		ls.emitEvent(LiveVADEvent{State: "silence", DurationMs: silence.DurationMs})

	case live.EventTypeInputCommitted:
		var committed live.InputCommittedMessage
		json.Unmarshal(data, &committed)
		ls.emitEvent(LiveInputCommittedEvent{Transcript: committed.Transcript})
		ls.emitEvent(LiveTranscriptEvent{
			Role:    "user",
			Text:    committed.Transcript,
			IsFinal: true,
		})

	case live.EventTypeTranscriptDelta:
		var delta live.TranscriptDeltaMessage
		json.Unmarshal(data, &delta)
		ls.emitEvent(LiveTranscriptEvent{
			Role:    "user",
			Text:    delta.Delta,
			IsFinal: false,
		})

	case live.EventTypeContentBlockStart:
		var start struct {
			Index        int `json:"index"`
			ContentBlock any `json:"content_block"`
		}
		json.Unmarshal(data, &start)
		ls.emitEvent(LiveContentBlockEvent{
			Event:        "start",
			Index:        start.Index,
			ContentBlock: start.ContentBlock,
		})

	case live.EventTypeContentBlockDelta:
		var delta struct {
			Index int `json:"index"`
			Delta any `json:"delta"`
		}
		json.Unmarshal(data, &delta)
		ls.emitEvent(LiveContentBlockEvent{
			Event: "delta",
			Index: delta.Index,
			Delta: delta.Delta,
		})

		// Extract text delta if present
		if deltaMap, ok := delta.Delta.(map[string]any); ok {
			if text, ok := deltaMap["text"].(string); ok && text != "" {
				ls.emitEvent(LiveTextDeltaEvent{Text: text})
			}
		}

	case live.EventTypeContentBlockStop:
		var stop struct {
			Index int `json:"index"`
		}
		json.Unmarshal(data, &stop)
		ls.emitEvent(LiveContentBlockEvent{
			Event: "stop",
			Index: stop.Index,
		})

	case live.EventTypeToolUse:
		var toolUse struct {
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		json.Unmarshal(data, &toolUse)
		ls.emitEvent(LiveToolCallEvent{
			ID:    toolUse.ID,
			Name:  toolUse.Name,
			Input: toolUse.Input,
		})
		// Attempt auto-execution
		go ls.executeToolIfRegistered(toolUse.ID, toolUse.Name, toolUse.Input)

	case live.EventTypeAudioDelta:
		var audioDelta live.AudioDeltaMessage
		json.Unmarshal(data, &audioDelta)
		audioData, _ := base64.StdEncoding.DecodeString(audioDelta.Data)
		if len(audioData) > 0 {
			sampleRate := 44100 // Default sample rate
			if ls.config != nil && ls.config.Voice != nil && ls.config.Voice.Output != nil && ls.config.Voice.Output.SampleRate > 0 {
				sampleRate = ls.config.Voice.Output.SampleRate
			}
			ls.emitEvent(LiveAudioEvent{Data: audioData, Format: audioDelta.Format, SampleRate: sampleRate, Channels: 1})
			select {
			case ls.audioOut <- audioData:
			default:
			}
		}

	case live.EventTypeInterruptDetecting:
		var detecting live.InterruptDetectingMessage
		json.Unmarshal(data, &detecting)
		ls.emitEvent(LiveInterruptEvent{
			State:      "detecting",
			Transcript: detecting.Transcript,
		})

	case live.EventTypeInterruptDismissed:
		var dismissed live.InterruptDismissedMessage
		json.Unmarshal(data, &dismissed)
		ls.emitEvent(LiveInterruptEvent{
			State:      "dismissed",
			Transcript: dismissed.Transcript,
			Reason:     dismissed.Reason,
		})

	case live.EventTypeResponseInterrupted:
		var interrupted live.ResponseInterruptedMessage
		json.Unmarshal(data, &interrupted)
		ls.emitEvent(LiveInterruptEvent{
			State:           "confirmed",
			Transcript:      interrupted.InterruptTranscript,
			PartialText:     interrupted.PartialText,
			AudioPositionMs: interrupted.AudioPositionMs,
		})

	case live.EventTypeMessageStop:
		var stop live.MessageStopMessage
		json.Unmarshal(data, &stop)
		ls.emitEvent(LiveMessageStopEvent{StopReason: stop.StopReason})

	case live.EventTypeError:
		var errMsg live.ErrorMessage
		json.Unmarshal(data, &errMsg)
		ls.emitEvent(LiveErrorEvent{
			Code:       errMsg.Code,
			Message:    errMsg.Message,
			RetryAfter: errMsg.RetryAfter,
		})
		if ls.onError != nil {
			ls.onError(fmt.Errorf("%s: %s", errMsg.Code, errMsg.Message))
		}
	}
}

func (ls *LiveStream) executeToolIfRegistered(id, name string, input map[string]any) {
	ls.toolHandlersMu.RLock()
	handler, exists := ls.toolHandlers[name]
	ls.toolHandlersMu.RUnlock()

	if !exists {
		return
	}

	// Execute handler
	inputJSON, _ := json.Marshal(input)
	output, err := handler(ls.ctx, inputJSON)

	// Send result back
	var content []any
	if err != nil {
		content = []any{map[string]any{
			"type": "text",
			"text": fmt.Sprintf("Error: %v", err),
		}}
	} else {
		switch v := output.(type) {
		case string:
			content = []any{map[string]any{"type": "text", "text": v}}
		case []types.ContentBlock:
			content = make([]any, len(v))
			for i, block := range v {
				content[i] = block
			}
		default:
			jsonBytes, _ := json.Marshal(v)
			content = []any{map[string]any{"type": "text", "text": string(jsonBytes)}}
		}
	}

	ls.SendToolResult(id, content)
}

// Events returns the channel of live events.
func (ls *LiveStream) Events() <-chan LiveEvent {
	return ls.events
}

// Audio returns the channel of outgoing TTS audio chunks.
func (ls *LiveStream) Audio() <-chan []byte {
	return ls.audioOut
}

// Transcripts returns the channel of transcript events.
func (ls *LiveStream) Transcripts() <-chan LiveTranscriptEvent {
	return ls.transcripts
}

// SessionID returns the session identifier.
func (ls *LiveStream) SessionID() string {
	return ls.sessionID
}

// IsReady returns true if the session is configured and ready.
func (ls *LiveStream) IsReady() bool {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()
	return ls.state == liveStateReady
}

// SendAudio sends audio data to the session.
// Audio should be 16-bit PCM at 16kHz mono for STT.
func (ls *LiveStream) SendAudio(data []byte) error {
	if ls.closed.Load() {
		return fmt.Errorf("stream closed")
	}

	// Direct mode: feed audio into the in-process session.
	if ls.directAdapter != nil {
		return ls.directAdapter.session.EnqueueAudio(data)
	}

	// Proxy mode: send via WebSocket
	ls.writeMu.Lock()
	defer ls.writeMu.Unlock()
	return ls.conn.WriteMessage(websocket.BinaryMessage, data)
}

// SendText sends text input to the session.
// This can be used for testing or text-based input.
func (ls *LiveStream) SendText(text string) error {
	// Direct mode: forward to the in-process session.
	if ls.directAdapter != nil {
		return ls.directAdapter.session.EnqueueText(text)
	}

	// Proxy mode: send via WebSocket
	return ls.sendJSON(live.InputTextMessage{
		Type: live.EventTypeInputText,
		Text: text,
	})
}

// Commit forces the end of the current user turn.
// Use this for push-to-talk mode.
func (ls *LiveStream) Commit() error {
	// Direct mode: force commit in the in-process session.
	if ls.directAdapter != nil {
		ls.directAdapter.session.ForceCommit()
		return nil
	}

	// Proxy mode: send via WebSocket
	return ls.sendJSON(live.InputCommitMessage{
		Type: live.EventTypeInputCommit,
	})
}

// Interrupt forces an interrupt, skipping semantic check.
// Optionally provide a transcript of what the user said.
func (ls *LiveStream) Interrupt(transcript string) error {
	if ls.directAdapter != nil {
		ls.directAdapter.session.ForceInterrupt(transcript)
		return nil
	}
	return ls.sendJSON(live.InputInterruptMessage{
		Type:       live.EventTypeInputInterrupt,
		Transcript: transcript,
	})
}

// SendToolResult sends a tool execution result back to the session.
func (ls *LiveStream) SendToolResult(toolUseID string, content []any) error {
	if ls.directAdapter != nil {
		return fmt.Errorf("tool results not yet supported for in-process live sessions")
	}
	return ls.sendJSON(live.ToolResultMessage{
		Type:      live.EventTypeToolResult,
		ToolUseID: toolUseID,
		Content:   content,
	})
}

// UpdateConfig updates the session configuration mid-session.
func (ls *LiveStream) UpdateConfig(cfg *LiveConfig) error {
	msg := live.SessionUpdateMessage{
		Type: live.EventTypeSessionUpdate,
		Config: &live.SessionConfig{
			Model:  cfg.Model,
			System: cfg.System,
		},
	}
	if len(cfg.Tools) > 0 {
		msg.Config.Tools = make([]any, len(cfg.Tools))
		for i, t := range cfg.Tools {
			msg.Config.Tools[i] = t
		}
	}
	if cfg.Voice != nil {
		msg.Config.Voice = convertVoiceConfig(cfg.Voice)
	}
	if ls.directAdapter != nil {
		return ls.directAdapter.session.UpdateConfig(msg.Config)
	}
	return ls.sendJSON(msg)
}

// Close closes the live stream.
func (ls *LiveStream) Close() error {
	if ls.closed.Swap(true) {
		return nil
	}
	ls.cancel()

	// Direct mode: close the in-process session.
	if ls.directAdapter != nil {
		if ls.directAdapter.session != nil {
			_ = ls.directAdapter.session.Close()
		}
		return nil
	}

	// Proxy mode: close WebSocket
	if ls.conn != nil {
		ls.writeMu.Lock()
		ls.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		ls.writeMu.Unlock()
		return ls.conn.Close()
	}

	return nil
}

// Done returns a channel that's closed when the stream ends.
func (ls *LiveStream) Done() <-chan struct{} {
	return ls.done
}

func (ls *LiveStream) sendJSON(v any) error {
	if ls.closed.Load() {
		return fmt.Errorf("stream closed")
	}
	if ls.directAdapter != nil {
		return fmt.Errorf("cannot send JSON in direct live mode")
	}
	ls.writeMu.Lock()
	defer ls.writeMu.Unlock()
	return ls.conn.WriteJSON(v)
}

func (ls *LiveStream) emitEvent(event LiveEvent) {
	if ls.closed.Load() {
		return
	}
	select {
	case ls.events <- event:
	case <-ls.done:
	default:
		// Buffer full, drop event
	}
}

func (ls *LiveStream) emitError(code, message string) {
	ls.emitEvent(LiveErrorEvent{Code: code, Message: message})
	if ls.onError != nil {
		ls.onError(fmt.Errorf("%s: %s", code, message))
	}
}

// --- Helper Functions ---

// CollectText is a helper that collects all text from the stream into a single string.
func (ls *LiveStream) CollectText(ctx context.Context) (string, error) {
	var builder strings.Builder
	for {
		select {
		case <-ctx.Done():
			return builder.String(), ctx.Err()
		case <-ls.done:
			return builder.String(), nil
		case event, ok := <-ls.events:
			if !ok {
				return builder.String(), nil
			}
			if textEvent, ok := event.(LiveTextDeltaEvent); ok {
				builder.WriteString(textEvent.Text)
			}
			if stopEvent, ok := event.(LiveMessageStopEvent); ok {
				_ = stopEvent
				return builder.String(), nil
			}
		}
	}
}

// ForEachEvent is a helper that calls fn for each event until the stream ends or fn returns false.
func (ls *LiveStream) ForEachEvent(ctx context.Context, fn func(LiveEvent) bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ls.done:
			return nil
		case event, ok := <-ls.events:
			if !ok {
				return nil
			}
			if !fn(event) {
				return nil
			}
		}
	}
}

// WaitForReady blocks until the session is ready or the context is cancelled.
func (ls *LiveStream) WaitForReady(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ls.done:
			return fmt.Errorf("stream closed before ready")
		case event, ok := <-ls.events:
			if !ok {
				return fmt.Errorf("stream closed before ready")
			}
			if _, ok := event.(LiveSessionCreatedEvent); ok {
				return nil
			}
			if errEvent, ok := event.(LiveErrorEvent); ok {
				return fmt.Errorf("%s: %s", errEvent.Code, errEvent.Message)
			}
		}
	}
}

// --- MessagesService Live method implementation ---

// liveConfig holds configuration for the Live connection.
type liveConfig struct {
	wsURL   string
	options []LiveStreamOption
}

// buildLiveURL constructs the WebSocket URL for live sessions.
func (c *Client) buildLiveURL() (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("live sessions require proxy mode (WithBaseURL)")
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	// Convert http(s) to ws(s)
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// Already WebSocket scheme
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/messages/live"

	// Add API key as query param if present
	if c.apiKey != "" {
		q := u.Query()
		q.Set("api_key", c.apiKey)
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// --- Direct Mode Live Session Support ---

// newLiveSessionDirect creates a live session running locally (direct mode).
// It runs the same core live session pipeline in-process (no proxy required).
func newLiveSessionDirect(ctx context.Context, cfg *LiveConfig, client *Client, opts ...LiveStreamOption) (*LiveStream, error) {
	if client.core == nil {
		return nil, fmt.Errorf("core engine not configured")
	}

	ctx, cancel := context.WithCancel(ctx)

	ls := &LiveStream{
		config:       cfg,
		events:       make(chan LiveEvent, 100),
		audioOut:     make(chan []byte, 100),
		transcripts:  make(chan LiveTranscriptEvent, 100),
		done:         make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		toolHandlers: make(map[string]ToolHandler),
		state:        liveStateConnecting,
	}

	for _, opt := range opts {
		opt(ls)
	}

	sessionID := fmt.Sprintf("direct_%d", time.Now().UnixNano())

	adapter := &directSessionAdapter{ls: ls}

	session := live.NewLiveSession(live.LiveSessionConfig{
		Connection: nil,
		SessionID:  sessionID,
		RunStreamCreator: func(ctx context.Context, config *live.SessionConfig, firstMessage string) (live.RunStreamInterface, error) {
			return liveadapters.NewEngineRunStreamAdapter(ctx, liveadapters.EngineRunStreamAdapterConfig{
				Engine:        client.core,
				SessionConfig: config,
				FirstMessage:  firstMessage,
				Logger:        client.logger,
			})
		},
		STTFactory: func(ctx context.Context, config *live.VoiceInputConfig) (live.STTStream, error) {
			if config == nil {
				return nil, nil
			}
			if client.voicePipeline == nil {
				return nil, fmt.Errorf("voice pipeline not configured; set CARTESIA_API_KEY or use WithProviderKey")
			}
			p := client.voicePipeline.STTProvider()
			if p == nil {
				return nil, fmt.Errorf("STT provider not available")
			}
			return liveadapters.NewStreamingSTTStream(ctx, liveadapters.StreamingSTTStreamConfig{
				Provider: p,
				Input:    config,
			})
		},
		TTSFactory: func(ctx context.Context, config *live.VoiceOutputConfig) (*tts.StreamingContext, error) {
			if config == nil {
				return nil, nil
			}
			if client.voicePipeline == nil {
				return nil, fmt.Errorf("voice pipeline not configured; set CARTESIA_API_KEY or use WithProviderKey")
			}
			p := client.voicePipeline.TTSProvider()
			if p == nil {
				return nil, fmt.Errorf("TTS provider not available")
			}
			return p.NewStreamingContext(ctx, tts.StreamingContextOptions{
				Voice:      config.Voice,
				Speed:      config.Speed,
				Format:     config.Format,
				SampleRate: config.SampleRate,
			})
		},
		LLMFunc: func(ctx context.Context, req live.LLMRequest) live.LLMResponse {
			msgReq := &types.MessageRequest{
				Model:     req.Model,
				MaxTokens: 100,
				Messages: []types.Message{
					{Role: "user", Content: req.Prompt},
				},
			}
			resp, err := client.core.CreateMessage(ctx, msgReq)
			if err != nil {
				return live.LLMResponse{Error: err}
			}
			return live.LLMResponse{Text: resp.TextContent()}
		},
	})

	adapter.session = session
	ls.directAdapter = adapter

	// Start processing loops first, then configure (Configure emits session.created).
	session.Start()
	go adapter.forwardLoop(ctx)

	coreCfg := &live.SessionConfig{
		Model:  cfg.Model,
		System: cfg.System,
		Tools:  nil,
		Voice:  convertVoiceConfig(cfg.Voice),
	}
	if len(cfg.Tools) > 0 {
		coreCfg.Tools = make([]any, len(cfg.Tools))
		for i := range cfg.Tools {
			coreCfg.Tools[i] = cfg.Tools[i]
		}
	}
	if err := session.Configure(coreCfg); err != nil {
		_ = session.Close()
		return nil, err
	}

	return ls, nil
}

type directSessionAdapter struct {
	ls      *LiveStream
	session *live.LiveSession
}

func (a *directSessionAdapter) forwardLoop(ctx context.Context) {
	defer func() {
		a.ls.closeOnce.Do(func() {
			close(a.ls.done)
			close(a.ls.events)
			close(a.ls.audioOut)
			close(a.ls.transcripts)
		})
		if a.ls.onDisconnect != nil {
			a.ls.onDisconnect()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.session.Done():
			return
		case event := <-a.session.Events():
			a.handleSessionEvent(event)
		}
	}
}

func (a *directSessionAdapter) handleSessionEvent(event live.SessionEvent) {
	switch e := event.(type) {
	case live.SessionCreatedEvent:
		a.ls.sessionID = e.SessionID
		a.ls.stateMu.Lock()
		a.ls.state = liveStateReady
		a.ls.stateMu.Unlock()
		a.ls.emitEvent(LiveSessionCreatedEvent{
			SessionID:  e.SessionID,
			Model:      e.Config.Model,
			SampleRate: e.Config.SampleRate,
			Channels:   e.Config.Channels,
		})
		if a.ls.onConnect != nil {
			a.ls.onConnect(e.SessionID)
		}

	case live.VADListeningEvent:
		a.ls.emitEvent(LiveVADEvent{State: "listening"})

	case live.VADAnalyzingEvent:
		a.ls.emitEvent(LiveVADEvent{State: "analyzing"})

	case live.VADSilenceEvent:
		a.ls.emitEvent(LiveVADEvent{State: "silence", DurationMs: e.DurationMs})

	case live.InputCommittedEvent:
		a.ls.emitEvent(LiveInputCommittedEvent{Transcript: e.Transcript})
		a.ls.emitEvent(LiveTranscriptEvent{Role: "user", Text: e.Transcript, IsFinal: true})

	case live.TranscriptDeltaEvent:
		a.ls.emitEvent(LiveTranscriptEvent{Role: "user", Text: e.Delta, IsFinal: false})

	case live.ContentBlockStartEvent:
		a.ls.emitEvent(LiveContentBlockEvent{Event: "start", Index: e.Index, ContentBlock: e.ContentBlock})

	case live.ContentBlockDeltaEvent:
		a.ls.emitEvent(LiveContentBlockEvent{Event: "delta", Index: e.Index, Delta: e.Delta})
		if deltaMap, ok := e.Delta.(map[string]any); ok {
			if text, ok := deltaMap["text"].(string); ok && text != "" {
				a.ls.emitEvent(LiveTextDeltaEvent{Text: text})
			}
		}

	case live.ContentBlockStopEvent:
		a.ls.emitEvent(LiveContentBlockEvent{Event: "stop", Index: e.Index})

	case live.ToolUseEvent:
		a.ls.emitEvent(LiveToolCallEvent{ID: e.ID, Name: e.Name, Input: e.Input})
		go a.ls.executeToolIfRegistered(e.ID, e.Name, e.Input)

	case live.AudioDeltaEvent:
		sampleRate := 24000
		if a.ls.config != nil && a.ls.config.Voice != nil && a.ls.config.Voice.Output != nil && a.ls.config.Voice.Output.SampleRate > 0 {
			sampleRate = a.ls.config.Voice.Output.SampleRate
		}
		a.ls.emitEvent(LiveAudioEvent{Data: e.Data, Format: e.Format, SampleRate: sampleRate, Channels: 1})
		select {
		case a.ls.audioOut <- e.Data:
		default:
		}

	case live.InterruptDetectingEvent:
		a.ls.emitEvent(LiveInterruptEvent{State: "detecting", Transcript: e.Transcript})

	case live.InterruptDismissedEvent:
		a.ls.emitEvent(LiveInterruptEvent{State: "dismissed", Transcript: e.Transcript, Reason: e.Reason})

	case live.ResponseInterruptedEvent:
		a.ls.emitEvent(LiveInterruptEvent{
			State:           "confirmed",
			Transcript:      e.InterruptTranscript,
			PartialText:     e.PartialText,
			AudioPositionMs: e.AudioPositionMs,
		})

	case live.MessageStopEvent:
		a.ls.emitEvent(LiveMessageStopEvent{StopReason: e.StopReason})

	case live.ErrorEvent:
		a.ls.emitEvent(LiveErrorEvent{Code: e.Code, Message: e.Message, RetryAfter: e.RetryAfter})
		if a.ls.onError != nil {
			a.ls.onError(fmt.Errorf("%s: %s", e.Code, e.Message))
		}
	}
}

// --- WebSocket URL Builder for Custom Endpoints ---

// LiveEndpoint configures a custom live session endpoint.
type LiveEndpoint struct {
	URL     string        // WebSocket URL
	Headers http.Header   // Custom headers
	Timeout time.Duration // Connection timeout
}

// newLiveStreamWithEndpoint creates a LiveStream with a custom endpoint configuration.
func newLiveStreamWithEndpoint(ctx context.Context, endpoint LiveEndpoint, cfg *LiveConfig, opts ...LiveStreamOption) (*LiveStream, error) {
	if endpoint.Timeout == 0 {
		endpoint.Timeout = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(ctx)

	ls := &LiveStream{
		config:       cfg,
		events:       make(chan LiveEvent, 100),
		audioOut:     make(chan []byte, 100),
		transcripts:  make(chan LiveTranscriptEvent, 100),
		done:         make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		toolHandlers: make(map[string]ToolHandler),
		state:        liveStateConnecting,
	}

	for _, opt := range opts {
		opt(ls)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: endpoint.Timeout,
	}

	conn, _, err := dialer.DialContext(ctx, endpoint.URL, endpoint.Headers)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	ls.conn = conn

	go ls.readLoop()

	if err := ls.sendConfig(); err != nil {
		ls.Close()
		return nil, fmt.Errorf("send config: %w", err)
	}

	return ls, nil
}

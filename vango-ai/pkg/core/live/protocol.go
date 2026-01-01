// Package live provides real-time bidirectional voice session functionality.
package live

// EventType constants for WebSocket message types.
const (
	// Client → Server events
	EventTypeSessionConfigure = "session.configure"
	EventTypeSessionUpdate    = "session.update"
	EventTypeInputInterrupt   = "input.interrupt"
	EventTypeInputCommit      = "input.commit"
	EventTypeInputText        = "input.text"
	EventTypeToolResult       = "tool_result"

	// Server → Client events
	EventTypeSessionCreated      = "session.created"
	EventTypeVADListening        = "vad.listening"
	EventTypeVADAnalyzing        = "vad.analyzing"
	EventTypeVADSilence          = "vad.silence"
	EventTypeInputCommitted      = "input.committed"
	EventTypeTranscriptDelta     = "transcript.delta"
	EventTypeContentBlockStart   = "content_block_start"
	EventTypeContentBlockDelta   = "content_block_delta"
	EventTypeContentBlockStop    = "content_block_stop"
	EventTypeToolUse             = "tool_use"
	EventTypeAudioDelta          = "audio_delta"
	EventTypeInterruptDetecting  = "interrupt.detecting"
	EventTypeInterruptDismissed  = "interrupt.dismissed"
	EventTypeResponseInterrupted = "response.interrupted"
	EventTypeMessageStop         = "message_stop"
	EventTypeError               = "error"
)

// SessionConfig defines the full agent configuration for a live session.
type SessionConfig struct {
	Model  string       `json:"model"`
	System string       `json:"system,omitempty"`
	Tools  []any        `json:"tools,omitempty"`
	Voice  *VoiceConfig `json:"voice,omitempty"`
}

// VoiceConfig contains all voice-related settings for a live session.
type VoiceConfig struct {
	Input     *VoiceInputConfig  `json:"input,omitempty"`
	Output    *VoiceOutputConfig `json:"output,omitempty"`
	VAD       *VADConfig         `json:"vad,omitempty"`
	Interrupt *InterruptConfig   `json:"interrupt,omitempty"`
}

// VoiceInputConfig configures speech-to-text.
type VoiceInputConfig struct {
	Provider string `json:"provider,omitempty"` // e.g., "cartesia"
	Model    string `json:"model,omitempty"`
	Language string `json:"language,omitempty"`
}

// VoiceOutputConfig configures text-to-speech.
type VoiceOutputConfig struct {
	Provider string  `json:"provider,omitempty"` // e.g., "cartesia"
	Voice    string  `json:"voice"`
	Speed    float64 `json:"speed,omitempty"`
	Format   string  `json:"format,omitempty"`
	// SampleRate is the output audio sample rate in Hz (default: 24000).
	SampleRate int `json:"sample_rate,omitempty"`
}

// VADConfig controls the hybrid voice activity detection.
type VADConfig struct {
	// Model is the fast LLM for semantic turn completion (default: "anthropic/claude-haiku-4-5-20251001")
	Model string `json:"model,omitempty"`

	// EnergyThreshold is the RMS threshold for silence detection (default: 0.02)
	EnergyThreshold float64 `json:"energy_threshold,omitempty"`

	// SilenceDurationMs is the silence duration before semantic check (default: 600)
	SilenceDurationMs int `json:"silence_duration_ms,omitempty"`

	// SemanticCheck enables semantic turn completion analysis (default: true)
	SemanticCheck *bool `json:"semantic_check,omitempty"`

	// MinWordsForCheck is the minimum words before semantic check (default: 2)
	MinWordsForCheck int `json:"min_words_for_check,omitempty"`

	// MaxSilenceMs is the force commit timeout (default: 3000)
	MaxSilenceMs int `json:"max_silence_ms,omitempty"`
}

// InterruptMode specifies how interrupts are detected.
type InterruptMode string

const (
	InterruptModeAuto     InterruptMode = "auto"     // Detect via VAD
	InterruptModeManual   InterruptMode = "manual"   // Only explicit input.interrupt
	InterruptModeDisabled InterruptMode = "disabled" // Bot always finishes
)

// SaveBehavior specifies how to handle partial responses when interrupted.
type SaveBehavior string

const (
	SaveBehaviorDiscard SaveBehavior = "discard" // Don't save partial response
	SaveBehaviorSave    SaveBehavior = "save"    // Save as-is
	SaveBehaviorMarked  SaveBehavior = "marked"  // Save with [interrupted] marker
)

// InterruptConfig controls barge-in detection.
type InterruptConfig struct {
	// Mode is the interrupt detection mode (default: "auto")
	Mode InterruptMode `json:"mode,omitempty"`

	// EnergyThreshold for detecting potential interrupt (higher than VAD, default: 0.05)
	EnergyThreshold float64 `json:"energy_threshold,omitempty"`

	// DebounceMs is the minimum sustained speech before check (default: 100)
	DebounceMs int `json:"debounce_ms,omitempty"`

	// SemanticCheck distinguishes interrupts from backchannels (default: true)
	SemanticCheck *bool `json:"semantic_check,omitempty"`

	// SemanticModel is the fast LLM for interrupt detection (default: "anthropic/claude-haiku-4-5-20251001")
	SemanticModel string `json:"semantic_model,omitempty"`

	// SavePartial specifies how to handle partial response (default: "marked")
	SavePartial SaveBehavior `json:"save_partial,omitempty"`
}

// --- Client → Server Messages ---

// SessionConfigureMessage is the first message to configure a session.
type SessionConfigureMessage struct {
	Type   string         `json:"type"` // "session.configure"
	Config *SessionConfig `json:"config"`
}

// SessionUpdateMessage updates configuration mid-session.
type SessionUpdateMessage struct {
	Type   string         `json:"type"` // "session.update"
	Config *SessionConfig `json:"config"`
}

// InputInterruptMessage forces an interrupt (skips semantic check).
type InputInterruptMessage struct {
	Type       string `json:"type"` // "input.interrupt"
	Transcript string `json:"transcript,omitempty"`
}

// InputCommitMessage forces end-of-turn (e.g., push-to-talk release).
type InputCommitMessage struct {
	Type string `json:"type"` // "input.commit"
}

// InputTextMessage sends text directly (for testing).
type InputTextMessage struct {
	Type string `json:"type"` // "input.text"
	Text string `json:"text"`
}

// ToolResultMessage returns a tool execution result.
type ToolResultMessage struct {
	Type      string `json:"type"` // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   []any  `json:"content"`
}

// --- Server → Client Messages ---

// SessionCreatedMessage confirms session creation.
type SessionCreatedMessage struct {
	Type      string            `json:"type"` // "session.created"
	SessionID string            `json:"session_id"`
	Config    SessionInfoConfig `json:"config"`
}

// SessionInfoConfig contains session info returned to client.
type SessionInfoConfig struct {
	Model      string `json:"model"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

// VADListeningMessage indicates ready for audio.
type VADListeningMessage struct {
	Type string `json:"type"` // "vad.listening"
}

// VADAnalyzingMessage indicates semantic check in progress.
type VADAnalyzingMessage struct {
	Type string `json:"type"` // "vad.analyzing"
}

// VADSilenceMessage indicates silence detected.
type VADSilenceMessage struct {
	Type       string `json:"type"` // "vad.silence"
	DurationMs int    `json:"duration_ms"`
}

// InputCommittedMessage indicates turn complete.
type InputCommittedMessage struct {
	Type       string `json:"type"` // "input.committed"
	Transcript string `json:"transcript"`
}

// TranscriptDeltaMessage contains real-time transcription.
type TranscriptDeltaMessage struct {
	Type  string `json:"type"` // "transcript.delta"
	Delta string `json:"delta"`
}

// AudioDeltaMessage contains audio output chunk.
type AudioDeltaMessage struct {
	Type   string `json:"type"` // "audio_delta"
	Data   string `json:"data"` // base64 encoded or can be binary frame
	Format string `json:"format,omitempty"`
}

// InterruptDetectingMessage indicates TTS paused, checking if real interrupt.
type InterruptDetectingMessage struct {
	Type       string `json:"type"` // "interrupt.detecting"
	Transcript string `json:"transcript"`
}

// InterruptDismissedMessage indicates not a real interrupt, TTS resuming.
type InterruptDismissedMessage struct {
	Type       string `json:"type"` // "interrupt.dismissed"
	Transcript string `json:"transcript"`
	Reason     string `json:"reason"` // "backchannel", "noise", etc.
}

// ResponseInterruptedMessage indicates real interrupt confirmed.
type ResponseInterruptedMessage struct {
	Type                string `json:"type"` // "response.interrupted"
	PartialText         string `json:"partial_text"`
	InterruptTranscript string `json:"interrupt_transcript"`
	AudioPositionMs     int    `json:"audio_position_ms"`
}

// MessageStopMessage indicates full response complete.
type MessageStopMessage struct {
	Type       string `json:"type"` // "message_stop"
	StopReason string `json:"stop_reason,omitempty"`
}

// ErrorMessage signals an error.
type ErrorMessage struct {
	Type       string `json:"type"` // "error"
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

// --- Default Configuration ---

// DefaultVADConfig returns the default VAD configuration.
func DefaultVADConfig() *VADConfig {
	semanticCheck := true
	return &VADConfig{
		Model:             "anthropic/claude-haiku-4-5-20251001",
		EnergyThreshold:   0.02,
		SilenceDurationMs: 600,
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  2,
		MaxSilenceMs:      3000,
	}
}

// DefaultInterruptConfig returns the default interrupt configuration.
func DefaultInterruptConfig() *InterruptConfig {
	semanticCheck := true
	return &InterruptConfig{
		Mode:            InterruptModeAuto,
		EnergyThreshold: 0.05,
		DebounceMs:      100,
		SemanticCheck:   &semanticCheck,
		SemanticModel:   "anthropic/claude-haiku-4-5-20251001",
		SavePartial:     SaveBehaviorMarked,
	}
}

// ApplyDefaults applies default values to the session config.
func (c *SessionConfig) ApplyDefaults() {
	if c.Voice == nil {
		c.Voice = &VoiceConfig{}
	}
	if c.Voice.Output != nil {
		if c.Voice.Output.Format == "" {
			c.Voice.Output.Format = "pcm"
		}
		if c.Voice.Output.SampleRate == 0 {
			c.Voice.Output.SampleRate = 24000
		}
	}
	if c.Voice.VAD == nil {
		c.Voice.VAD = DefaultVADConfig()
	} else {
		applyVADDefaults(c.Voice.VAD)
	}
	if c.Voice.Interrupt == nil {
		c.Voice.Interrupt = DefaultInterruptConfig()
	} else {
		applyInterruptDefaults(c.Voice.Interrupt)
	}
}

func applyVADDefaults(cfg *VADConfig) {
	defaults := DefaultVADConfig()
	if cfg.Model == "" {
		cfg.Model = defaults.Model
	}
	if cfg.EnergyThreshold == 0 {
		cfg.EnergyThreshold = defaults.EnergyThreshold
	}
	if cfg.SilenceDurationMs == 0 {
		cfg.SilenceDurationMs = defaults.SilenceDurationMs
	}
	if cfg.SemanticCheck == nil {
		cfg.SemanticCheck = defaults.SemanticCheck
	}
	if cfg.MinWordsForCheck == 0 {
		cfg.MinWordsForCheck = defaults.MinWordsForCheck
	}
	if cfg.MaxSilenceMs == 0 {
		cfg.MaxSilenceMs = defaults.MaxSilenceMs
	}
}

func applyInterruptDefaults(cfg *InterruptConfig) {
	defaults := DefaultInterruptConfig()
	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}
	if cfg.EnergyThreshold == 0 {
		cfg.EnergyThreshold = defaults.EnergyThreshold
	}
	if cfg.DebounceMs == 0 {
		cfg.DebounceMs = defaults.DebounceMs
	}
	if cfg.SemanticCheck == nil {
		cfg.SemanticCheck = defaults.SemanticCheck
	}
	if cfg.SemanticModel == "" {
		cfg.SemanticModel = defaults.SemanticModel
	}
	if cfg.SavePartial == "" {
		cfg.SavePartial = defaults.SavePartial
	}
}

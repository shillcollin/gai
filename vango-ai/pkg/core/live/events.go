package live

// SessionEvent is an event from a live session.
type SessionEvent interface {
	sessionEventType() string
}

// --- Server-generated events ---

// SessionCreatedEvent signals session initialization.
type SessionCreatedEvent struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

func (e SessionCreatedEvent) sessionEventType() string { return EventTypeSessionCreated }

// VADListeningEvent indicates ready for audio input.
type VADListeningEvent struct{}

func (e VADListeningEvent) sessionEventType() string { return EventTypeVADListening }

// VADAnalyzingEvent indicates semantic analysis in progress.
type VADAnalyzingEvent struct{}

func (e VADAnalyzingEvent) sessionEventType() string { return EventTypeVADAnalyzing }

// VADSilenceEvent indicates silence detected.
type VADSilenceEvent struct {
	DurationMs int `json:"duration_ms"`
}

func (e VADSilenceEvent) sessionEventType() string { return EventTypeVADSilence }

// InputCommittedEvent signals turn complete with transcript.
type InputCommittedEvent struct {
	Transcript string `json:"transcript"`
}

func (e InputCommittedEvent) sessionEventType() string { return EventTypeInputCommitted }

// TranscriptDeltaEvent contains incremental transcription.
type TranscriptDeltaEvent struct {
	Delta string `json:"delta"`
}

func (e TranscriptDeltaEvent) sessionEventType() string { return EventTypeTranscriptDelta }

// AudioDeltaEvent contains an audio output chunk.
type AudioDeltaEvent struct {
	Data   []byte `json:"data"`
	Format string `json:"format,omitempty"`
}

func (e AudioDeltaEvent) sessionEventType() string { return EventTypeAudioDelta }

// InterruptDetectingEvent indicates TTS paused, checking if real interrupt.
type InterruptDetectingEvent struct {
	Transcript string `json:"transcript"`
}

func (e InterruptDetectingEvent) sessionEventType() string { return EventTypeInterruptDetecting }

// InterruptDismissedEvent indicates not a real interrupt, TTS resuming.
type InterruptDismissedEvent struct {
	Transcript string `json:"transcript"`
	Reason     string `json:"reason"` // "backchannel", "noise", etc.
}

func (e InterruptDismissedEvent) sessionEventType() string { return EventTypeInterruptDismissed }

// ResponseInterruptedEvent indicates real interrupt confirmed.
type ResponseInterruptedEvent struct {
	PartialText         string `json:"partial_text"`
	InterruptTranscript string `json:"interrupt_transcript"`
	AudioPositionMs     int    `json:"audio_position_ms"`
}

func (e ResponseInterruptedEvent) sessionEventType() string { return EventTypeResponseInterrupted }

// MessageStopEvent signals full response complete.
type MessageStopEvent struct {
	StopReason string `json:"stop_reason,omitempty"`
}

func (e MessageStopEvent) sessionEventType() string { return EventTypeMessageStop }

// ErrorEvent signals an error.
type ErrorEvent struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

func (e ErrorEvent) sessionEventType() string { return EventTypeError }

// ContentBlockStartEvent signals start of a content block.
type ContentBlockStartEvent struct {
	Index        int `json:"index"`
	ContentBlock any `json:"content_block"`
}

func (e ContentBlockStartEvent) sessionEventType() string { return EventTypeContentBlockStart }

// ContentBlockDeltaEvent signals content block update.
type ContentBlockDeltaEvent struct {
	Index int `json:"index"`
	Delta any `json:"delta"`
}

func (e ContentBlockDeltaEvent) sessionEventType() string { return EventTypeContentBlockDelta }

// ContentBlockStopEvent signals content block complete.
type ContentBlockStopEvent struct {
	Index int `json:"index"`
}

func (e ContentBlockStopEvent) sessionEventType() string { return EventTypeContentBlockStop }

// ToolUseEvent signals a tool call request.
type ToolUseEvent struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (e ToolUseEvent) sessionEventType() string { return EventTypeToolUse }

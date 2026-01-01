// Package live provides real-time bidirectional voice session functionality.
package live

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

// TTSPipeline manages text-to-speech output with pause/resume and cancel support.
// It wraps a TTS StreamingContext and adds buffering and state management
// for interrupt handling in live sessions.
type TTSPipeline struct {
	// Configuration
	sampleRate int
	format     string

	// State (protected by mu)
	mu            sync.Mutex
	paused        bool
	cancelled     atomic.Bool
	pausePoint    int64 // Byte offset where we paused
	audioPosition int64 // Current audio position in milliseconds
	textPosition  int   // Character position in text stream

	// Pending audio chunks when paused
	pendingChunks [][]byte
	pendingMu     sync.Mutex

	// Underlying TTS context (nil if not active)
	ttsCtx *tts.StreamingContext

	// Output channel for audio chunks
	audioOut     chan []byte
	audioOutOnce sync.Once

	// Text buffer for batching
	textBuffer   string
	textBufferMu sync.Mutex

	// Callbacks
	onAudioChunk func([]byte)

	// Lifecycle
	done      chan struct{}
	closeOnce sync.Once
}

// TTSPipelineConfig configures the TTS pipeline.
type TTSPipelineConfig struct {
	// SampleRate in Hz (default: 24000)
	SampleRate int

	// Format: "wav", "mp3", "pcm" (default: "pcm")
	Format string

	// OnAudioChunk is called for each audio chunk (optional)
	OnAudioChunk func([]byte)
}

// NewTTSPipeline creates a new TTS pipeline.
func NewTTSPipeline(config TTSPipelineConfig) *TTSPipeline {
	sampleRate := config.SampleRate
	if sampleRate == 0 {
		sampleRate = 24000
	}
	format := config.Format
	if format == "" {
		format = "pcm"
	}

	return &TTSPipeline{
		sampleRate:    sampleRate,
		format:        format,
		audioOut:      make(chan []byte, 100),
		done:          make(chan struct{}),
		onAudioChunk:  config.OnAudioChunk,
		pendingChunks: make([][]byte, 0),
	}
}

// SetTTSContext sets the underlying TTS streaming context.
// This should be called when starting a new TTS generation.
func (p *TTSPipeline) SetTTSContext(ctx *tts.StreamingContext) {
	p.mu.Lock()
	p.ttsCtx = ctx
	p.paused = false
	p.cancelled.Store(false)
	p.pausePoint = 0
	p.audioPosition = 0
	p.pendingChunks = p.pendingChunks[:0]
	p.mu.Unlock()

	// Forward audio from TTS context
	if ctx != nil {
		go p.forwardAudio(ctx)
	}
}

// forwardAudio forwards audio from the TTS context to the output.
func (p *TTSPipeline) forwardAudio(ctx *tts.StreamingContext) {
	for chunk := range ctx.Audio() {
		if p.cancelled.Load() {
			continue // Drain but discard
		}

		p.mu.Lock()
		paused := p.paused
		p.mu.Unlock()

		if paused {
			// Buffer the chunk for later
			p.pendingMu.Lock()
			p.pendingChunks = append(p.pendingChunks, chunk)
			p.pendingMu.Unlock()
		} else {
			p.sendChunk(chunk)
		}
	}
}

// sendChunk sends a chunk and updates position tracking.
func (p *TTSPipeline) sendChunk(chunk []byte) {
	if p.cancelled.Load() {
		return
	}

	// Update audio position
	// For 16-bit mono PCM: position_ms = bytes / (sample_rate * 2) * 1000
	bytesPerMs := float64(p.sampleRate) * 2 / 1000
	durationMs := int64(float64(len(chunk)) / bytesPerMs)

	p.mu.Lock()
	p.audioPosition += durationMs
	p.mu.Unlock()

	// Call callback if set
	if p.onAudioChunk != nil {
		p.onAudioChunk(chunk)
	}

	// Send to output channel
	select {
	case p.audioOut <- chunk:
	case <-p.done:
	}
}

// SendText sends text to be synthesized.
// The text is forwarded to the underlying TTS context.
func (p *TTSPipeline) SendText(text string) error {
	p.mu.Lock()
	ttsCtx := p.ttsCtx
	paused := p.paused
	cancelled := p.cancelled.Load()
	p.mu.Unlock()

	if cancelled || ttsCtx == nil {
		return nil
	}

	if paused {
		// Buffer text while paused
		p.textBufferMu.Lock()
		p.textBuffer += text
		p.textBufferMu.Unlock()
		return nil
	}

	return ttsCtx.SendText(text, false)
}

// Flush signals that all text has been sent.
func (p *TTSPipeline) Flush() error {
	p.mu.Lock()
	ttsCtx := p.ttsCtx
	p.mu.Unlock()

	if ttsCtx == nil {
		return nil
	}
	return ttsCtx.Flush()
}

// Pause immediately pauses audio output.
// Audio continues to be generated and buffered.
// Returns the current audio position in milliseconds.
func (p *TTSPipeline) Pause() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.paused {
		return p.audioPosition
	}

	p.paused = true
	p.pausePoint = p.audioPosition
	return p.audioPosition
}

// Resume continues audio output from where it was paused.
// Buffered audio chunks are sent immediately.
func (p *TTSPipeline) Resume() {
	p.mu.Lock()
	if !p.paused {
		p.mu.Unlock()
		return
	}
	p.paused = false
	p.mu.Unlock()

	// Send any buffered text
	p.textBufferMu.Lock()
	bufferedText := p.textBuffer
	p.textBuffer = ""
	p.textBufferMu.Unlock()

	if bufferedText != "" {
		p.mu.Lock()
		ttsCtx := p.ttsCtx
		p.mu.Unlock()
		if ttsCtx != nil {
			ttsCtx.SendText(bufferedText, false)
		}
	}

	// Send any pending audio chunks
	p.pendingMu.Lock()
	chunks := make([][]byte, len(p.pendingChunks))
	copy(chunks, p.pendingChunks)
	p.pendingChunks = p.pendingChunks[:0]
	p.pendingMu.Unlock()

	for _, chunk := range chunks {
		p.sendChunk(chunk)
	}
}

// Cancel stops the TTS pipeline and discards all pending audio.
// Returns the partial text position (for reporting interrupted speech).
func (p *TTSPipeline) Cancel() (audioPositionMs int64) {
	p.cancelled.Store(true)

	p.mu.Lock()
	audioPositionMs = p.audioPosition
	p.paused = false
	p.pausePoint = 0
	p.audioPosition = 0
	ttsCtx := p.ttsCtx
	p.ttsCtx = nil
	p.mu.Unlock()

	// Clear pending chunks
	p.pendingMu.Lock()
	p.pendingChunks = p.pendingChunks[:0]
	p.pendingMu.Unlock()

	// Clear text buffer
	p.textBufferMu.Lock()
	p.textBuffer = ""
	p.textBufferMu.Unlock()

	// Close the underlying context
	if ttsCtx != nil {
		ttsCtx.Close()
	}

	return audioPositionMs
}

// Reset resets the pipeline for a new generation.
func (p *TTSPipeline) Reset() {
	p.Cancel()
	p.cancelled.Store(false)
}

// IsPaused returns whether the pipeline is currently paused.
func (p *TTSPipeline) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// IsCancelled returns whether the pipeline has been cancelled.
func (p *TTSPipeline) IsCancelled() bool {
	return p.cancelled.Load()
}

// AudioPosition returns the current audio position in milliseconds.
func (p *TTSPipeline) AudioPosition() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.audioPosition
}

// Audio returns the channel for receiving audio chunks.
func (p *TTSPipeline) Audio() <-chan []byte {
	return p.audioOut
}

// Close closes the TTS pipeline.
func (p *TTSPipeline) Close() error {
	p.closeOnce.Do(func() {
		p.Cancel()
		close(p.done)
		p.audioOutOnce.Do(func() {
			close(p.audioOut)
		})
	})
	return nil
}

// --- TTSPipelineState for state machine integration ---

// TTSPipelineState represents the current state of the TTS pipeline.
type TTSPipelineState int

const (
	// TTSStateIdle indicates no TTS is active.
	TTSStateIdle TTSPipelineState = iota

	// TTSStateGenerating indicates TTS is actively generating and sending audio.
	TTSStateGenerating

	// TTSStatePaused indicates TTS is paused (audio buffered but not sent).
	TTSStatePaused

	// TTSStateCancelled indicates TTS was cancelled.
	TTSStateCancelled
)

// State returns the current state of the pipeline.
func (p *TTSPipeline) State() TTSPipelineState {
	if p.cancelled.Load() {
		return TTSStateCancelled
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ttsCtx == nil {
		return TTSStateIdle
	}
	if p.paused {
		return TTSStatePaused
	}
	return TTSStateGenerating
}

// --- InterruptHandler for integration with live sessions ---

// InterruptHandler coordinates TTS pipeline with interrupt detection.
type InterruptHandler struct {
	pipeline *TTSPipeline
	detector *InterruptDetector
	llmFunc  LLMFunc
	saveMode SaveBehavior
	mu       sync.Mutex

	// Callback for interrupt events
	onInterruptDetecting func(transcript string)
	onInterruptDismissed func(transcript string, reason string)
	onInterruptConfirmed func(partialText string, interruptTranscript string, audioPositionMs int64)
}

// InterruptHandlerConfig configures the interrupt handler.
type InterruptHandlerConfig struct {
	Pipeline             *TTSPipeline
	Detector             *InterruptDetector
	SaveMode             SaveBehavior
	OnInterruptDetecting func(transcript string)
	OnInterruptDismissed func(transcript string, reason string)
	OnInterruptConfirmed func(partialText string, interruptTranscript string, audioPositionMs int64)
}

// NewInterruptHandler creates a new interrupt handler.
func NewInterruptHandler(config InterruptHandlerConfig) *InterruptHandler {
	return &InterruptHandler{
		pipeline:             config.Pipeline,
		detector:             config.Detector,
		saveMode:             config.SaveMode,
		onInterruptDetecting: config.OnInterruptDetecting,
		onInterruptDismissed: config.OnInterruptDismissed,
		onInterruptConfirmed: config.OnInterruptConfirmed,
	}
}

// HandlePotentialInterrupt handles a potential interrupt during TTS.
// It pauses TTS, runs semantic check, and either resumes or confirms interrupt.
// This implements the "pause first, decide second" strategy.
//
// The partialText parameter is the text generated so far by the LLM.
// The transcript parameter is what the user said (potential interrupt).
func (h *InterruptHandler) HandlePotentialInterrupt(ctx context.Context, partialText string, transcript string) InterruptResult {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Step 1: Immediately pause TTS
	audioPos := h.pipeline.Pause()

	// Notify that we're checking
	if h.onInterruptDetecting != nil {
		h.onInterruptDetecting(transcript)
	}

	// Step 2: Run semantic check
	result := h.detector.CheckInterrupt(ctx, transcript)

	if result.IsInterrupt {
		// Step 3a: Confirmed interrupt - cancel TTS
		h.pipeline.Cancel()

		if h.onInterruptConfirmed != nil {
			h.onInterruptConfirmed(partialText, transcript, audioPos)
		}
	} else {
		// Step 3b: Not an interrupt (backchannel) - resume TTS
		h.pipeline.Resume()

		if h.onInterruptDismissed != nil {
			h.onInterruptDismissed(transcript, result.Reason)
		}
	}

	return result
}

// ForceInterrupt immediately interrupts without semantic check.
// Used when the user explicitly signals an interrupt.
func (h *InterruptHandler) ForceInterrupt(partialText string, transcript string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	audioPos := h.pipeline.Cancel()

	if h.onInterruptConfirmed != nil {
		h.onInterruptConfirmed(partialText, transcript, audioPos)
	}
}

// --- Helper for calculating audio duration ---

// AudioDurationMs calculates the duration in milliseconds for PCM audio.
// Assumes 16-bit mono PCM.
func AudioDurationMs(audioBytes int, sampleRate int) int64 {
	if sampleRate == 0 {
		sampleRate = 24000
	}
	bytesPerSecond := sampleRate * 2 // 16-bit = 2 bytes per sample
	return int64(audioBytes) * 1000 / int64(bytesPerSecond)
}

// BytesForDurationMs calculates the number of bytes for a given duration.
// Assumes 16-bit mono PCM.
func BytesForDurationMs(durationMs int64, sampleRate int) int {
	if sampleRate == 0 {
		sampleRate = 24000
	}
	bytesPerSecond := sampleRate * 2 // 16-bit = 2 bytes per sample
	return int(durationMs * int64(bytesPerSecond) / 1000)
}

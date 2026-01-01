package vango

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// --- Event Type Helpers ---

// IsAudioEvent returns true if the event contains audio data.
func IsAudioEvent(event LiveEvent) bool {
	_, ok := event.(LiveAudioEvent)
	return ok
}

// IsTextEvent returns true if the event contains text data.
func IsTextEvent(event LiveEvent) bool {
	_, ok := event.(LiveTextDeltaEvent)
	return ok
}

// IsTranscriptEvent returns true if the event is a transcript.
func IsTranscriptEvent(event LiveEvent) bool {
	_, ok := event.(LiveTranscriptEvent)
	return ok
}

// IsToolCallEvent returns true if the event is a tool call.
func IsToolCallEvent(event LiveEvent) bool {
	_, ok := event.(LiveToolCallEvent)
	return ok
}

// IsErrorEvent returns true if the event is an error.
func IsErrorEvent(event LiveEvent) bool {
	_, ok := event.(LiveErrorEvent)
	return ok
}

// IsInterruptEvent returns true if the event is an interrupt-related event.
func IsInterruptEvent(event LiveEvent) bool {
	_, ok := event.(LiveInterruptEvent)
	return ok
}

// AsAudioEvent converts an event to LiveAudioEvent if possible.
func AsAudioEvent(event LiveEvent) (LiveAudioEvent, bool) {
	e, ok := event.(LiveAudioEvent)
	return e, ok
}

// AsTextDeltaEvent converts an event to LiveTextDeltaEvent if possible.
func AsTextDeltaEvent(event LiveEvent) (LiveTextDeltaEvent, bool) {
	e, ok := event.(LiveTextDeltaEvent)
	return e, ok
}

// AsTranscriptEvent converts an event to LiveTranscriptEvent if possible.
func AsTranscriptEvent(event LiveEvent) (LiveTranscriptEvent, bool) {
	e, ok := event.(LiveTranscriptEvent)
	return e, ok
}

// AsToolCallEvent converts an event to LiveToolCallEvent if possible.
func AsToolCallEvent(event LiveEvent) (LiveToolCallEvent, bool) {
	e, ok := event.(LiveToolCallEvent)
	return e, ok
}

// AsErrorEvent converts an event to LiveErrorEvent if possible.
func AsErrorEvent(event LiveEvent) (LiveErrorEvent, bool) {
	e, ok := event.(LiveErrorEvent)
	return e, ok
}

// AsInterruptEvent converts an event to LiveInterruptEvent if possible.
func AsInterruptEvent(event LiveEvent) (LiveInterruptEvent, bool) {
	e, ok := event.(LiveInterruptEvent)
	return e, ok
}

// AsMessageStopEvent converts an event to LiveMessageStopEvent if possible.
func AsMessageStopEvent(event LiveEvent) (LiveMessageStopEvent, bool) {
	e, ok := event.(LiveMessageStopEvent)
	return e, ok
}

// --- Configuration Builders ---

// NewLiveConfig creates a new LiveConfig with the specified model.
func NewLiveConfig(model string) *LiveConfig {
	return &LiveConfig{
		Model: model,
	}
}

// WithSystem adds a system prompt to the config.
func (c *LiveConfig) WithSystem(system string) *LiveConfig {
	c.System = system
	return c
}

// WithVoice configures voice settings.
func (c *LiveConfig) WithVoice(voice string) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	if c.Voice.Output == nil {
		c.Voice.Output = &LiveVoiceOutputConfig{}
	}
	c.Voice.Output.Voice = voice
	return c
}

// WithVoiceOutput fully configures voice output.
func (c *LiveConfig) WithVoiceOutput(voice string, format string, speed float64) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	c.Voice.Output = &LiveVoiceOutputConfig{
		Voice:  voice,
		Format: format,
		Speed:  speed,
	}
	return c
}

// WithVoiceInput configures voice input (STT).
func (c *LiveConfig) WithVoiceInput(provider, model, language string) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	c.Voice.Input = &LiveVoiceInputConfig{
		Provider: provider,
		Model:    model,
		Language: language,
	}
	return c
}

// WithInterruptMode sets the interrupt detection mode.
func (c *LiveConfig) WithInterruptMode(mode LiveInterruptMode) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	if c.Voice.Interrupt == nil {
		c.Voice.Interrupt = &LiveInterruptConfig{}
	}
	c.Voice.Interrupt.Mode = mode
	return c
}

// WithVADThreshold sets the VAD energy threshold.
func (c *LiveConfig) WithVADThreshold(threshold float64) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	if c.Voice.VAD == nil {
		c.Voice.VAD = &LiveVADConfig{}
	}
	c.Voice.VAD.EnergyThreshold = threshold
	return c
}

// WithVADSilenceDuration sets the silence duration before turn commit.
func (c *LiveConfig) WithVADSilenceDuration(durationMs int) *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	if c.Voice.VAD == nil {
		c.Voice.VAD = &LiveVADConfig{}
	}
	c.Voice.VAD.SilenceDurationMs = durationMs
	return c
}

// DisableSemanticCheck disables semantic turn completion check.
func (c *LiveConfig) DisableSemanticCheck() *LiveConfig {
	if c.Voice == nil {
		c.Voice = &LiveVoiceConfig{}
	}
	if c.Voice.VAD == nil {
		c.Voice.VAD = &LiveVADConfig{}
	}
	enabled := false
	c.Voice.VAD.SemanticCheck = &enabled
	return c
}

// --- Audio Helpers ---

// AudioFormat describes audio encoding parameters.
type AudioFormat struct {
	SampleRate int    // Samples per second (e.g., 24000)
	Channels   int    // Number of channels (1 for mono)
	BitDepth   int    // Bits per sample (e.g., 16)
	Encoding   string // "pcm", "wav", "mp3", etc.
}

// DefaultLiveAudioFormat returns the default audio format for live sessions.
func DefaultLiveAudioFormat() AudioFormat {
	return AudioFormat{
		SampleRate: 24000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm",
	}
}

// PCMToWAVWithFormat converts raw PCM audio data to WAV format using an AudioFormat.
// For simple cases, use the existing PCMToWAV function directly.
func PCMToWAVWithFormat(pcmData []byte, format AudioFormat) []byte {
	if format.Channels == 0 {
		format.Channels = 1
	}
	if format.SampleRate == 0 {
		format.SampleRate = 24000
	}
	if format.BitDepth == 0 {
		format.BitDepth = 16
	}
	return PCMToWAV(pcmData, format.SampleRate, format.BitDepth, format.Channels)
}

// SplitAudioIntoChunks splits audio data into chunks of the specified duration.
func SplitAudioIntoChunks(audioData []byte, format AudioFormat, chunkDurationMs int) [][]byte {
	if chunkDurationMs <= 0 {
		chunkDurationMs = 100
	}

	bytesPerSample := format.BitDepth / 8
	bytesPerSecond := format.SampleRate * format.Channels * bytesPerSample
	chunkSize := (bytesPerSecond * chunkDurationMs) / 1000

	// Ensure chunk size is aligned to sample boundaries
	sampleSize := format.Channels * bytesPerSample
	chunkSize = (chunkSize / sampleSize) * sampleSize

	if chunkSize <= 0 {
		return [][]byte{audioData}
	}

	var chunks [][]byte
	for i := 0; i < len(audioData); i += chunkSize {
		end := i + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}
		chunks = append(chunks, audioData[i:end])
	}

	return chunks
}

// --- Event Processing Helpers ---

// EventHandler handles events from a LiveStream.
type EventHandler struct {
	OnAudio         func(LiveAudioEvent)
	OnTextDelta     func(LiveTextDeltaEvent)
	OnTranscript    func(LiveTranscriptEvent)
	OnToolCall      func(LiveToolCallEvent)
	OnInterrupt     func(LiveInterruptEvent)
	OnMessageStop   func(LiveMessageStopEvent)
	OnError         func(LiveErrorEvent)
	OnVAD           func(LiveVADEvent)
	OnInputCommitted func(LiveInputCommittedEvent)
	OnSessionCreated func(LiveSessionCreatedEvent)
	OnContentBlock  func(LiveContentBlockEvent)
}

// ProcessEvents processes events from a LiveStream using the provided handlers.
func (ls *LiveStream) ProcessEvents(ctx context.Context, handler EventHandler) error {
	return ls.ForEachEvent(ctx, func(event LiveEvent) bool {
		switch e := event.(type) {
		case LiveAudioEvent:
			if handler.OnAudio != nil {
				handler.OnAudio(e)
			}
		case LiveTextDeltaEvent:
			if handler.OnTextDelta != nil {
				handler.OnTextDelta(e)
			}
		case LiveTranscriptEvent:
			if handler.OnTranscript != nil {
				handler.OnTranscript(e)
			}
		case LiveToolCallEvent:
			if handler.OnToolCall != nil {
				handler.OnToolCall(e)
			}
		case LiveInterruptEvent:
			if handler.OnInterrupt != nil {
				handler.OnInterrupt(e)
			}
		case LiveMessageStopEvent:
			if handler.OnMessageStop != nil {
				handler.OnMessageStop(e)
			}
		case LiveErrorEvent:
			if handler.OnError != nil {
				handler.OnError(e)
			}
		case LiveVADEvent:
			if handler.OnVAD != nil {
				handler.OnVAD(e)
			}
		case LiveInputCommittedEvent:
			if handler.OnInputCommitted != nil {
				handler.OnInputCommitted(e)
			}
		case LiveSessionCreatedEvent:
			if handler.OnSessionCreated != nil {
				handler.OnSessionCreated(e)
			}
		case LiveContentBlockEvent:
			if handler.OnContentBlock != nil {
				handler.OnContentBlock(e)
			}
		}
		return true
	})
}

// --- Audio Streaming Helpers ---

// AudioWriter provides a convenient way to send audio data to a LiveStream.
type AudioWriter struct {
	stream      *LiveStream
	chunkSize   int
	buffer      []byte
	bufferMu    sync.Mutex
	ticker      *time.Ticker
	done        chan struct{}
	started     bool
	sendRate    time.Duration
}

// NewAudioWriter creates an AudioWriter for sending audio to a LiveStream.
// chunkDurationMs specifies the duration of each audio chunk in milliseconds.
func NewAudioWriter(stream *LiveStream, chunkDurationMs int) *AudioWriter {
	format := DefaultLiveAudioFormat()
	bytesPerMs := (format.SampleRate * format.Channels * (format.BitDepth / 8)) / 1000
	chunkSize := bytesPerMs * chunkDurationMs

	return &AudioWriter{
		stream:    stream,
		chunkSize: chunkSize,
		buffer:    make([]byte, 0, chunkSize*2),
		done:      make(chan struct{}),
		sendRate:  time.Duration(chunkDurationMs) * time.Millisecond,
	}
}

// Write implements io.Writer for streaming audio data.
func (w *AudioWriter) Write(p []byte) (n int, err error) {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()

	w.buffer = append(w.buffer, p...)

	// Start ticker on first write
	if !w.started {
		w.started = true
		w.ticker = time.NewTicker(w.sendRate)
		go w.sendLoop()
	}

	return len(p), nil
}

func (w *AudioWriter) sendLoop() {
	for {
		select {
		case <-w.done:
			return
		case <-w.ticker.C:
			w.sendChunk()
		}
	}
}

func (w *AudioWriter) sendChunk() {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()

	if len(w.buffer) < w.chunkSize {
		return
	}

	chunk := make([]byte, w.chunkSize)
	copy(chunk, w.buffer[:w.chunkSize])
	w.buffer = w.buffer[w.chunkSize:]

	w.stream.SendAudio(chunk)
}

// Flush sends any remaining buffered audio.
func (w *AudioWriter) Flush() error {
	w.bufferMu.Lock()
	defer w.bufferMu.Unlock()

	if len(w.buffer) > 0 {
		err := w.stream.SendAudio(w.buffer)
		w.buffer = w.buffer[:0]
		return err
	}
	return nil
}

// Close stops the audio writer and flushes remaining data.
func (w *AudioWriter) Close() error {
	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.done)
	return w.Flush()
}

// --- Audio Reader Helpers ---

// AudioReader collects audio output from a LiveStream.
type AudioReader struct {
	stream  *LiveStream
	buffer  bytes.Buffer
	bufferMu sync.Mutex
	done     chan struct{}
	started  bool
}

// NewAudioReader creates an AudioReader for collecting TTS output.
func NewAudioReader(stream *LiveStream) *AudioReader {
	return &AudioReader{
		stream: stream,
		done:   make(chan struct{}),
	}
}

// Start begins collecting audio from the stream.
func (r *AudioReader) Start(ctx context.Context) {
	if r.started {
		return
	}
	r.started = true

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.done:
				return
			case chunk, ok := <-r.stream.Audio():
				if !ok {
					return
				}
				r.bufferMu.Lock()
				r.buffer.Write(chunk)
				r.bufferMu.Unlock()
			}
		}
	}()
}

// Read implements io.Reader for reading collected audio.
func (r *AudioReader) Read(p []byte) (n int, err error) {
	r.bufferMu.Lock()
	defer r.bufferMu.Unlock()

	if r.buffer.Len() == 0 {
		return 0, io.EOF
	}
	return r.buffer.Read(p)
}

// Bytes returns all collected audio data.
func (r *AudioReader) Bytes() []byte {
	r.bufferMu.Lock()
	defer r.bufferMu.Unlock()
	return r.buffer.Bytes()
}

// Close stops collecting audio.
func (r *AudioReader) Close() error {
	close(r.done)
	return nil
}

// --- Text Collection Helpers ---

// TextCollector collects all text deltas from a LiveStream.
type TextCollector struct {
	builder strings.Builder
	mu      sync.Mutex
	stream  *LiveStream
	done    chan struct{}
}

// NewTextCollector creates a TextCollector for a LiveStream.
func NewTextCollector(stream *LiveStream) *TextCollector {
	return &TextCollector{
		stream: stream,
		done:   make(chan struct{}),
	}
}

// Start begins collecting text from the stream.
func (c *TextCollector) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case event, ok := <-c.stream.Events():
				if !ok {
					return
				}
				if textEvent, ok := event.(LiveTextDeltaEvent); ok {
					c.mu.Lock()
					c.builder.WriteString(textEvent.Text)
					c.mu.Unlock()
				}
			}
		}
	}()
}

// Text returns the collected text so far.
func (c *TextCollector) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.builder.String()
}

// Close stops collecting text.
func (c *TextCollector) Close() error {
	close(c.done)
	return nil
}

// --- Convenience Functions ---

// QuickLive is a convenience function for simple live interactions.
// It sends text, waits for a response, and returns the response text.
func QuickLive(ctx context.Context, stream *LiveStream, text string) (string, error) {
	if err := stream.SendText(text); err != nil {
		return "", fmt.Errorf("send text: %w", err)
	}

	if err := stream.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	var response strings.Builder
	for {
		select {
		case <-ctx.Done():
			return response.String(), ctx.Err()
		case <-stream.Done():
			return response.String(), nil
		case event, ok := <-stream.Events():
			if !ok {
				return response.String(), nil
			}
			switch e := event.(type) {
			case LiveTextDeltaEvent:
				response.WriteString(e.Text)
			case LiveMessageStopEvent:
				return response.String(), nil
			case LiveErrorEvent:
				return response.String(), fmt.Errorf("%s: %s", e.Code, e.Message)
			}
		}
	}
}

// StreamAudioToLive streams audio from a reader to a LiveStream.
func StreamAudioToLive(ctx context.Context, stream *LiveStream, audio io.Reader, chunkDurationMs int) error {
	writer := NewAudioWriter(stream, chunkDurationMs)
	defer writer.Close()

	buf := make([]byte, writer.chunkSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := audio.Read(buf)
		if err == io.EOF {
			return writer.Flush()
		}
		if err != nil {
			return err
		}

		if _, err := writer.Write(buf[:n]); err != nil {
			return err
		}
	}
}

// CollectAudioFromLive collects all audio output until the message stops.
func CollectAudioFromLive(ctx context.Context, stream *LiveStream) ([]byte, error) {
	var buffer bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			return buffer.Bytes(), ctx.Err()
		case <-stream.Done():
			return buffer.Bytes(), nil
		case chunk, ok := <-stream.Audio():
			if !ok {
				return buffer.Bytes(), nil
			}
			buffer.Write(chunk)
		case event, ok := <-stream.Events():
			if !ok {
				return buffer.Bytes(), nil
			}
			if _, ok := event.(LiveMessageStopEvent); ok {
				// Drain remaining audio
				for {
					select {
					case chunk, ok := <-stream.Audio():
						if !ok {
							return buffer.Bytes(), nil
						}
						buffer.Write(chunk)
					default:
						return buffer.Bytes(), nil
					}
				}
			}
			if errEvent, ok := event.(LiveErrorEvent); ok {
				return buffer.Bytes(), fmt.Errorf("%s: %s", errEvent.Code, errEvent.Message)
			}
		}
	}
}

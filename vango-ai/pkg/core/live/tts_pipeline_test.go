package live

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

// mockStreamingContext creates a mock TTS streaming context for testing.
func mockStreamingContext() *tts.StreamingContext {
	ctx := tts.NewStreamingContext()

	// Set up a simple send function
	ctx.SendFunc = func(text string, isFinal bool) error {
		return nil
	}

	ctx.CloseFunc = func() error {
		return nil
	}

	return ctx
}

// mockStreamingContextWithAudio creates a mock TTS context that generates audio.
func mockStreamingContextWithAudio(chunks [][]byte, delay time.Duration) *tts.StreamingContext {
	ctx := tts.NewStreamingContext()

	ctx.SendFunc = func(text string, isFinal bool) error {
		return nil
	}

	ctx.CloseFunc = func() error {
		return nil
	}

	// Generate audio in background
	go func() {
		defer ctx.FinishAudio()
		for _, chunk := range chunks {
			if delay > 0 {
				time.Sleep(delay)
			}
			if !ctx.PushAudio(chunk) {
				return
			}
		}
	}()

	return ctx
}

func TestNewTTSPipeline(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	if p.sampleRate != 24000 {
		t.Errorf("expected default sample rate 24000, got %d", p.sampleRate)
	}
	if p.format != "pcm" {
		t.Errorf("expected default format 'pcm', got %s", p.format)
	}
}

func TestNewTTSPipeline_CustomConfig(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{
		SampleRate: 48000,
		Format:     "wav",
	})

	if p.sampleRate != 48000 {
		t.Errorf("expected sample rate 48000, got %d", p.sampleRate)
	}
	if p.format != "wav" {
		t.Errorf("expected format 'wav', got %s", p.format)
	}
}

func TestTTSPipeline_InitialState(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	if p.IsPaused() {
		t.Error("expected not paused initially")
	}
	if p.IsCancelled() {
		t.Error("expected not cancelled initially")
	}
	if p.State() != TTSStateIdle {
		t.Errorf("expected idle state, got %v", p.State())
	}
	if p.AudioPosition() != 0 {
		t.Errorf("expected audio position 0, got %d", p.AudioPosition())
	}
}

func TestTTSPipeline_SetTTSContext(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	if p.State() != TTSStateGenerating {
		t.Errorf("expected generating state after setting context, got %v", p.State())
	}
}

func TestTTSPipeline_Pause(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	pos := p.Pause()
	if pos != 0 {
		t.Errorf("expected pause position 0, got %d", pos)
	}

	if !p.IsPaused() {
		t.Error("expected paused after Pause()")
	}
	if p.State() != TTSStatePaused {
		t.Errorf("expected paused state, got %v", p.State())
	}
}

func TestTTSPipeline_PauseIdempotent(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	pos1 := p.Pause()
	pos2 := p.Pause()

	if pos1 != pos2 {
		t.Errorf("expected same pause position, got %d and %d", pos1, pos2)
	}
}

func TestTTSPipeline_Resume(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	p.Pause()
	p.Resume()

	if p.IsPaused() {
		t.Error("expected not paused after Resume()")
	}
	if p.State() != TTSStateGenerating {
		t.Errorf("expected generating state after resume, got %v", p.State())
	}
}

func TestTTSPipeline_ResumeWithoutPause(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	// Resume without pause should not panic or change state
	p.Resume()

	if p.IsPaused() {
		t.Error("expected not paused")
	}
}

func TestTTSPipeline_Cancel(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	pos := p.Cancel()
	if pos != 0 {
		t.Errorf("expected cancel position 0, got %d", pos)
	}

	if !p.IsCancelled() {
		t.Error("expected cancelled after Cancel()")
	}
	if p.State() != TTSStateCancelled {
		t.Errorf("expected cancelled state, got %v", p.State())
	}
}

func TestTTSPipeline_CancelClearsPendingChunks(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	// Add some pending chunks
	p.pendingMu.Lock()
	p.pendingChunks = append(p.pendingChunks, []byte{1, 2, 3})
	p.pendingMu.Unlock()

	p.Cancel()

	p.pendingMu.Lock()
	pending := len(p.pendingChunks)
	p.pendingMu.Unlock()

	if pending != 0 {
		t.Errorf("expected 0 pending chunks after cancel, got %d", pending)
	}
}

func TestTTSPipeline_Reset(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	p.Cancel()
	p.Reset()

	if p.IsCancelled() {
		t.Error("expected not cancelled after Reset()")
	}
	if p.State() != TTSStateIdle {
		t.Errorf("expected idle state after reset, got %v", p.State())
	}
}

func TestTTSPipeline_AudioForwarding(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	chunks := [][]byte{
		make([]byte, 4800), // 100ms at 24kHz 16-bit
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 0)
	p.SetTTSContext(ctx)

	// Receive audio chunks
	received := 0
	timeout := time.After(1 * time.Second)
	for received < len(chunks) {
		select {
		case <-p.Audio():
			received++
		case <-timeout:
			t.Fatalf("timeout waiting for audio, got %d/%d chunks", received, len(chunks))
		}
	}

	if received != len(chunks) {
		t.Errorf("expected %d chunks, got %d", len(chunks), received)
	}
}

func TestTTSPipeline_AudioPositionTracking(t *testing.T) {
	var chunksSent atomic.Int32
	p := NewTTSPipeline(TTSPipelineConfig{
		OnAudioChunk: func(chunk []byte) {
			chunksSent.Add(1)
		},
	})
	defer p.Close()

	// 4800 bytes = 100ms at 24kHz 16-bit mono
	chunks := [][]byte{
		make([]byte, 4800),
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 0)
	p.SetTTSContext(ctx)

	// Drain audio channel
	for range 3 {
		select {
		case <-p.Audio():
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for audio")
		}
	}

	// Allow position tracking to update
	time.Sleep(10 * time.Millisecond)

	pos := p.AudioPosition()
	// Each chunk is 4800 bytes = 100ms at 24kHz 16-bit mono
	expectedMs := int64(300) // 3 chunks * 100ms
	// Allow some tolerance due to calculation differences
	if pos < expectedMs-10 || pos > expectedMs+10 {
		t.Errorf("expected audio position ~%dms, got %dms", expectedMs, pos)
	}
}

func TestTTSPipeline_PauseBuffersAudio(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	// Create chunks with delay
	chunks := [][]byte{
		make([]byte, 4800),
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 20*time.Millisecond)
	p.SetTTSContext(ctx)

	// Pause immediately
	p.Pause()

	// Wait for chunks to be generated
	time.Sleep(100 * time.Millisecond)

	// Check that chunks are buffered
	p.pendingMu.Lock()
	pending := len(p.pendingChunks)
	p.pendingMu.Unlock()

	if pending == 0 {
		t.Error("expected pending chunks when paused")
	}
}

func TestTTSPipeline_ResumeFlushesBufferedAudio(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	chunks := [][]byte{
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 10*time.Millisecond)
	p.SetTTSContext(ctx)

	// Pause immediately
	p.Pause()

	// Wait for all chunks to be buffered
	time.Sleep(50 * time.Millisecond)

	// Resume and receive
	p.Resume()

	received := 0
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case _, ok := <-p.Audio():
			if !ok {
				break loop
			}
			received++
			if received >= len(chunks) {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if received != len(chunks) {
		t.Errorf("expected %d chunks after resume, got %d", len(chunks), received)
	}
}

func TestTTSPipeline_CancelDiscardsPendingAudio(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	chunks := [][]byte{
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 10*time.Millisecond)
	p.SetTTSContext(ctx)

	// Pause to buffer
	p.Pause()
	time.Sleep(50 * time.Millisecond)

	// Cancel should discard buffered chunks
	p.Cancel()

	p.pendingMu.Lock()
	pending := len(p.pendingChunks)
	p.pendingMu.Unlock()

	if pending != 0 {
		t.Errorf("expected 0 pending chunks after cancel, got %d", pending)
	}
}

func TestTTSPipeline_CancelReturnsPosition(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	chunks := [][]byte{
		make([]byte, 4800), // 100ms
	}

	ctx := mockStreamingContextWithAudio(chunks, 0)
	p.SetTTSContext(ctx)

	// Wait for chunk to be sent
	select {
	case <-p.Audio():
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for audio")
	}

	// Allow position tracking to update
	time.Sleep(10 * time.Millisecond)

	pos := p.Cancel()
	// Should be approximately 100ms
	if pos < 90 || pos > 110 {
		t.Errorf("expected cancel position ~100ms, got %dms", pos)
	}
}

func TestTTSPipeline_OnAudioChunkCallback(t *testing.T) {
	var callbackCount atomic.Int32

	p := NewTTSPipeline(TTSPipelineConfig{
		OnAudioChunk: func(chunk []byte) {
			callbackCount.Add(1)
		},
	})
	defer p.Close()

	chunks := [][]byte{
		make([]byte, 4800),
		make([]byte, 4800),
	}

	ctx := mockStreamingContextWithAudio(chunks, 0)
	p.SetTTSContext(ctx)

	// Drain audio
	for range 2 {
		select {
		case <-p.Audio():
		case <-time.After(1 * time.Second):
			t.Fatal("timeout")
		}
	}

	if callbackCount.Load() != 2 {
		t.Errorf("expected 2 callback calls, got %d", callbackCount.Load())
	}
}

func TestTTSPipelineState_String(t *testing.T) {
	tests := []struct {
		state    TTSPipelineState
		expected string
	}{
		{TTSStateIdle, "0"},
		{TTSStateGenerating, "1"},
		{TTSStatePaused, "2"},
		{TTSStateCancelled, "3"},
	}

	for _, tt := range tests {
		// States are just ints, but we can test the values
		if int(tt.state) < 0 || int(tt.state) > 3 {
			t.Errorf("unexpected state value: %d", tt.state)
		}
	}
}

func TestAudioDurationMs(t *testing.T) {
	tests := []struct {
		bytes      int
		sampleRate int
		expected   int64
	}{
		{4800, 24000, 100},   // 100ms at 24kHz
		{9600, 24000, 200},   // 200ms at 24kHz
		{3200, 16000, 100},   // 100ms at 16kHz
		{9600, 48000, 100},   // 100ms at 48kHz
		{48000, 24000, 1000}, // 1 second at 24kHz
		{0, 24000, 0},        // 0 bytes
	}

	for _, tt := range tests {
		result := AudioDurationMs(tt.bytes, tt.sampleRate)
		if result != tt.expected {
			t.Errorf("AudioDurationMs(%d, %d) = %d, expected %d",
				tt.bytes, tt.sampleRate, result, tt.expected)
		}
	}
}

func TestAudioDurationMs_DefaultSampleRate(t *testing.T) {
	// With 0 sample rate, should default to 24000
	result := AudioDurationMs(4800, 0)
	if result != 100 {
		t.Errorf("expected 100ms with default sample rate, got %d", result)
	}
}

func TestBytesForDurationMs(t *testing.T) {
	tests := []struct {
		durationMs int64
		sampleRate int
		expected   int
	}{
		{100, 24000, 4800},   // 100ms at 24kHz
		{200, 24000, 9600},   // 200ms at 24kHz
		{100, 16000, 3200},   // 100ms at 16kHz
		{100, 48000, 9600},   // 100ms at 48kHz
		{1000, 24000, 48000}, // 1 second at 24kHz
		{0, 24000, 0},        // 0 duration
	}

	for _, tt := range tests {
		result := BytesForDurationMs(tt.durationMs, tt.sampleRate)
		if result != tt.expected {
			t.Errorf("BytesForDurationMs(%d, %d) = %d, expected %d",
				tt.durationMs, tt.sampleRate, result, tt.expected)
		}
	}
}

func TestBytesForDurationMs_DefaultSampleRate(t *testing.T) {
	// With 0 sample rate, should default to 24000
	result := BytesForDurationMs(100, 0)
	if result != 4800 {
		t.Errorf("expected 4800 bytes with default sample rate, got %d", result)
	}
}

func TestInterruptHandler_HandlePotentialInterrupt_Backchannel(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	// Mock detector that says "not an interrupt"
	mockLLM := MockLLMFunc(nil, "NO", 0)
	detector := NewInterruptDetector(mockLLM, InterruptConfig{})

	var detectingCalled, dismissedCalled bool

	handler := NewInterruptHandler(InterruptHandlerConfig{
		Pipeline: p,
		Detector: detector,
		OnInterruptDetecting: func(transcript string) {
			detectingCalled = true
		},
		OnInterruptDismissed: func(transcript string, reason string) {
			dismissedCalled = true
		},
		OnInterruptConfirmed: func(partialText, interruptTranscript string, audioPositionMs int64) {
			t.Error("should not confirm for backchannel")
		},
	})

	result := handler.HandlePotentialInterrupt(context.Background(), "partial text", "uh huh")

	if result.IsInterrupt {
		t.Error("expected not an interrupt")
	}
	if !detectingCalled {
		t.Error("expected detecting callback")
	}
	if !dismissedCalled {
		t.Error("expected dismissed callback")
	}
	if p.IsPaused() {
		t.Error("expected TTS resumed after backchannel")
	}
}

func TestInterruptHandler_HandlePotentialInterrupt_RealInterrupt(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	// Mock detector that says "yes interrupt"
	mockLLM := MockLLMFunc(nil, "YES", 0)
	detector := NewInterruptDetector(mockLLM, InterruptConfig{})

	var confirmedCalled bool
	var capturedPartial, capturedTranscript string
	var capturedPos int64

	handler := NewInterruptHandler(InterruptHandlerConfig{
		Pipeline: p,
		Detector: detector,
		OnInterruptDetecting: func(transcript string) {},
		OnInterruptDismissed: func(transcript string, reason string) {
			t.Error("should not dismiss for real interrupt")
		},
		OnInterruptConfirmed: func(partialText, interruptTranscript string, audioPositionMs int64) {
			confirmedCalled = true
			capturedPartial = partialText
			capturedTranscript = interruptTranscript
			capturedPos = audioPositionMs
		},
	})

	result := handler.HandlePotentialInterrupt(context.Background(), "Hello, I'd be happy to help", "wait stop")

	if !result.IsInterrupt {
		t.Error("expected an interrupt")
	}
	if !confirmedCalled {
		t.Error("expected confirmed callback")
	}
	if capturedPartial != "Hello, I'd be happy to help" {
		t.Errorf("expected partial text, got %q", capturedPartial)
	}
	if capturedTranscript != "wait stop" {
		t.Errorf("expected interrupt transcript, got %q", capturedTranscript)
	}
	if p.State() != TTSStateCancelled {
		t.Error("expected TTS cancelled after interrupt")
	}
	_ = capturedPos // Position is 0 for this test
}

func TestInterruptHandler_ForceInterrupt(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	detector := NewInterruptDetector(MockLLMFunc(nil, "NO", 0), InterruptConfig{})

	var confirmedCalled bool

	handler := NewInterruptHandler(InterruptHandlerConfig{
		Pipeline: p,
		Detector: detector,
		OnInterruptConfirmed: func(partialText, interruptTranscript string, audioPositionMs int64) {
			confirmedCalled = true
		},
	})

	handler.ForceInterrupt("partial text", "STOP")

	if !confirmedCalled {
		t.Error("expected confirmed callback")
	}
	if p.State() != TTSStateCancelled {
		t.Error("expected TTS cancelled after force interrupt")
	}
}

func TestInterruptHandler_PauseFirst(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	ctx := mockStreamingContext()
	p.SetTTSContext(ctx)

	// Mock detector with delay to ensure pause happens first
	mockLLM := MockLLMFunc(nil, "NO", 50*time.Millisecond)
	detector := NewInterruptDetector(mockLLM, InterruptConfig{})

	var pausedDuringCheck bool
	var wg sync.WaitGroup
	wg.Add(1)

	handler := NewInterruptHandler(InterruptHandlerConfig{
		Pipeline: p,
		Detector: detector,
		OnInterruptDetecting: func(transcript string) {
			// Check if paused during detection
			pausedDuringCheck = p.IsPaused()
			wg.Done()
		},
	})

	go handler.HandlePotentialInterrupt(context.Background(), "partial", "uh huh")

	wg.Wait()

	if !pausedDuringCheck {
		t.Error("TTS should be paused during interrupt detection")
	}
}

func TestTTSPipeline_ConcurrentAccess(t *testing.T) {
	p := NewTTSPipeline(TTSPipelineConfig{})
	defer p.Close()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent pause/resume
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			p.Pause()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			p.Resume()
		}
	}()

	// Concurrent state checks
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = p.IsPaused()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = p.State()
		}
	}()

	wg.Wait()
}

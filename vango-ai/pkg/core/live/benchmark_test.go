package live

import (
	"context"
	"testing"
	"time"
)

// --- Latency Benchmarks ---

// BenchmarkVAD_EnergyOnly benchmarks VAD with energy-only detection.
func BenchmarkVAD_EnergyOnly(b *testing.B) {
	semanticCheck := false
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 600,
		SemanticCheck:     &semanticCheck,
	})

	speech := generatePCM(0.1, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vad.ProcessAudio(speech, "hello world")
	}
}

// BenchmarkVAD_WithSemanticCheck benchmarks VAD with semantic checking.
func BenchmarkVAD_WithSemanticCheck(b *testing.B) {
	// Simulate a 10ms LLM response (much faster than real)
	llmFunc := MockLLMFunc(nil, "YES", 10*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	semanticCheck := true
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 10, // Short for benchmark
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  1,
	}, checker)

	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vad.Reset()
		vad.ProcessAudio(speech, "hello world")
		vad.ProcessAudio(silence, "")
		time.Sleep(15 * time.Millisecond) // Wait for silence threshold
		vad.ProcessAudio(silence, "")
	}
}

// BenchmarkSemanticChecker benchmarks the semantic checker alone.
func BenchmarkSemanticChecker(b *testing.B) {
	llmFunc := MockLLMFunc(nil, "YES", 0) // No latency for pure processing benchmark
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckTurnComplete("Book me a flight to Paris please.")
	}
}

// BenchmarkSemanticChecker_WithLatency simulates realistic LLM latency.
func BenchmarkSemanticChecker_WithLatency(b *testing.B) {
	// Simulate 150ms Haiku latency
	llmFunc := MockLLMFunc(nil, "YES", 150*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckTurnComplete("Book me a flight to Paris please.")
	}
}

// BenchmarkInterruptDetector benchmarks interrupt detection.
func BenchmarkInterruptDetector(b *testing.B) {
	llmFunc := MockLLMFunc(nil, "NO", 0)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.CheckInterrupt(context.Background(), "uh huh")
	}
}

// BenchmarkInterruptDetector_WithLatency simulates realistic LLM latency.
func BenchmarkInterruptDetector_WithLatency(b *testing.B) {
	// Simulate 150ms Haiku latency
	llmFunc := MockLLMFunc(nil, "NO", 150*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.CheckInterrupt(context.Background(), "uh huh")
	}
}

// BenchmarkAudioBuffer_Write benchmarks audio buffer write.
func BenchmarkAudioBuffer_Write(b *testing.B) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)
	pcm := generatePCM(0.1, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(pcm)
	}
}

// BenchmarkAudioBuffer_Energy benchmarks energy calculation.
func BenchmarkAudioBuffer_Energy(b *testing.B) {
	pcm := generatePCM(0.1, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateEnergy(pcm)
	}
}

// BenchmarkAudioBuffer_HasSustainedEnergy benchmarks sustained energy check.
func BenchmarkAudioBuffer_HasSustainedEnergy(b *testing.B) {
	format := DefaultAudioFormat()
	buf := NewAudioBuffer(format, time.Second)

	// Pre-populate with data
	speech := generatePCM(0.1, 1000)
	for i := 0; i < 20; i++ {
		buf.Write(speech)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.HasSustainedEnergy(0.02, 100*time.Millisecond)
	}
}

// --- Latency Tests (not benchmarks, but latency assertions) ---

// TestSemanticChecker_Latency_UnderBudget verifies semantic check completes in time.
func TestSemanticChecker_Latency_UnderBudget(t *testing.T) {
	// Target: < 350ms with Haiku (per spec)
	// We simulate 200ms LLM latency
	llmFunc := MockLLMFunc(nil, "YES", 200*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{
		Timeout: 500 * time.Millisecond,
	})

	start := time.Now()
	_, err := checker.CheckTurnComplete("Hello, how are you doing today?")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if elapsed > 350*time.Millisecond {
		t.Errorf("semantic check took %v, expected < 350ms", elapsed)
	}
	t.Logf("Semantic check latency: %v", elapsed)
}

// TestInterruptDetector_Latency_UnderBudget verifies interrupt detection completes in time.
func TestInterruptDetector_Latency_UnderBudget(t *testing.T) {
	// Target: < 350ms with Haiku (per spec)
	llmFunc := MockLLMFunc(nil, "NO", 200*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	start := time.Now()
	result := detector.CheckInterrupt(context.Background(), "uh huh")
	elapsed := time.Since(start)

	if result.Reason == "error_fallback" {
		t.Fatal("unexpected error")
	}

	if elapsed > 350*time.Millisecond {
		t.Errorf("interrupt detection took %v, expected < 350ms", elapsed)
	}
	t.Logf("Interrupt detection latency: %v", elapsed)
}

// TestVAD_EnergyOnly_Latency_UnderBudget verifies energy-only VAD is fast.
func TestVAD_EnergyOnly_Latency_UnderBudget(t *testing.T) {
	// Target: < 10ms per spec
	semanticCheck := false
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 600,
		SemanticCheck:     &semanticCheck,
	})

	speech := generatePCM(0.1, 1000)

	start := time.Now()
	for i := 0; i < 100; i++ {
		vad.ProcessAudio(speech, "hello")
	}
	elapsed := time.Since(start)
	perCall := elapsed / 100

	if perCall > 10*time.Millisecond {
		t.Errorf("VAD energy check took %v per call, expected < 10ms", perCall)
	}
	t.Logf("VAD energy-only latency: %v per call", perCall)
}

// TestTTSPause_Latency measures TTS pause latency.
func TestTTSPause_Latency(t *testing.T) {
	// Target: < 5ms per spec
	pipeline := NewTTSPipeline(TTSPipelineConfig{
		SampleRate: 24000,
		Format:     "pcm",
	})
	defer pipeline.Close()

	start := time.Now()
	pipeline.Pause()
	elapsed := time.Since(start)

	if elapsed > 5*time.Millisecond {
		t.Errorf("TTS pause took %v, expected < 5ms", elapsed)
	}
	t.Logf("TTS pause latency: %v", elapsed)
}

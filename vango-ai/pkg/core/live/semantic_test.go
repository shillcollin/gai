package live

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLLMSemanticChecker_CheckTurnComplete_Yes(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 10*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	complete, err := checker.CheckTurnComplete("Book me a flight to Paris please.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !complete {
		t.Error("expected turn to be complete")
	}

	// Check metrics
	metrics := checker.Metrics()
	if metrics.TotalCalls != 1 {
		t.Errorf("expected 1 call, got %d", metrics.TotalCalls)
	}
	if metrics.TotalErrors != 0 {
		t.Errorf("expected 0 errors, got %d", metrics.TotalErrors)
	}
}

func TestLLMSemanticChecker_CheckTurnComplete_No(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "NO", 10*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	complete, err := checker.CheckTurnComplete("Book me a flight to")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if complete {
		t.Error("expected turn to be incomplete")
	}
}

func TestLLMSemanticChecker_CheckTurnComplete_CaseInsensitive(t *testing.T) {
	tests := []struct {
		response string
		expected bool
	}{
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		{"yes.", true},
		{"YES!", true},
		{"no", false},
		{"NO", false},
		{"No", false},
		{"maybe", false},
		{"", false},
	}

	for _, tc := range tests {
		llmFunc := MockLLMFunc(nil, tc.response, 0)
		checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

		complete, err := checker.CheckTurnComplete("test transcript")
		if err != nil {
			t.Fatalf("unexpected error for response %q: %v", tc.response, err)
		}
		if complete != tc.expected {
			t.Errorf("for response %q: expected %v, got %v", tc.response, tc.expected, complete)
		}
	}
}

func TestLLMSemanticChecker_CheckTurnComplete_Error(t *testing.T) {
	expectedErr := errors.New("LLM unavailable")
	llmFunc := ErrorLLMFunc(expectedErr)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	_, err := checker.CheckTurnComplete("test")
	if err == nil {
		t.Error("expected error")
	}
	if err != expectedErr {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}

	metrics := checker.Metrics()
	if metrics.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", metrics.TotalErrors)
	}
}

func TestLLMSemanticChecker_Timeout(t *testing.T) {
	// LLM takes 200ms but timeout is 50ms
	llmFunc := MockLLMFunc(nil, "YES", 200*time.Millisecond)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{
		Timeout: 50 * time.Millisecond,
	})

	start := time.Now()
	_, err := checker.CheckTurnComplete("test")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("timeout didn't work, took %v", elapsed)
	}
}

func TestLLMSemanticChecker_CustomModel(t *testing.T) {
	var capturedModel string
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		capturedModel = req.Model
		return LLMResponse{Text: "YES"}
	}

	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{
		Model: "custom/model",
	})

	checker.CheckTurnComplete("test")

	if capturedModel != "custom/model" {
		t.Errorf("expected model 'custom/model', got %q", capturedModel)
	}
}

func TestLLMSemanticChecker_DefaultModel(t *testing.T) {
	var capturedModel string
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		capturedModel = req.Model
		return LLMResponse{Text: "YES"}
	}

	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})
	checker.CheckTurnComplete("test")

	if capturedModel != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("expected default Haiku model, got %q", capturedModel)
	}
}

func TestLLMSemanticChecker_PromptFormat(t *testing.T) {
	var capturedPrompt string
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		capturedPrompt = req.Prompt
		return LLMResponse{Text: "YES"}
	}

	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})
	checker.CheckTurnComplete("Hello, how are you?")

	if !strings.Contains(capturedPrompt, "Hello, how are you?") {
		t.Error("prompt should contain the transcript")
	}
	if !strings.Contains(capturedPrompt, "Reply only: YES or NO") {
		t.Error("prompt should ask for YES/NO response")
	}
}

func TestLLMSemanticChecker_ResetMetrics(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 0)
	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	// Make some calls
	checker.CheckTurnComplete("test 1")
	checker.CheckTurnComplete("test 2")

	metrics := checker.Metrics()
	if metrics.TotalCalls != 2 {
		t.Errorf("expected 2 calls, got %d", metrics.TotalCalls)
	}

	// Reset
	checker.ResetMetrics()
	metrics = checker.Metrics()
	if metrics.TotalCalls != 0 {
		t.Errorf("expected 0 calls after reset, got %d", metrics.TotalCalls)
	}
}

// --- Interrupt Detector Tests ---

func TestInterruptDetector_RealInterrupt(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 10*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	result := detector.CheckInterrupt(context.Background(), "Actually wait")

	if !result.IsInterrupt {
		t.Error("expected interrupt")
	}
	if result.Reason != "interrupt" {
		t.Errorf("expected reason 'interrupt', got %q", result.Reason)
	}
}

func TestInterruptDetector_Backchannel(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "NO", 10*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	result := detector.CheckInterrupt(context.Background(), "uh huh")

	if result.IsInterrupt {
		t.Error("expected backchannel, not interrupt")
	}
	if result.Reason != "backchannel" {
		t.Errorf("expected reason 'backchannel', got %q", result.Reason)
	}
}

func TestInterruptDetector_TooShort(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 10*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	// Single character should be skipped
	result := detector.CheckInterrupt(context.Background(), "a")

	if result.IsInterrupt {
		t.Error("too short utterance should not be interrupt")
	}
	if result.Reason != "too_short" {
		t.Errorf("expected reason 'too_short', got %q", result.Reason)
	}
}

func TestInterruptDetector_SemanticDisabled(t *testing.T) {
	semanticCheck := false
	llmFunc := MockLLMFunc(nil, "NO", 10*time.Millisecond)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{
		SemanticCheck: &semanticCheck,
	})

	// With semantic check disabled, any speech should be treated as interrupt
	result := detector.CheckInterrupt(context.Background(), "uh huh")

	if !result.IsInterrupt {
		t.Error("with semantic disabled, should treat as interrupt")
	}
	if result.Reason != "semantic_disabled" {
		t.Errorf("expected reason 'semantic_disabled', got %q", result.Reason)
	}
}

func TestInterruptDetector_Error_FallsBackToInterrupt(t *testing.T) {
	llmFunc := ErrorLLMFunc(errors.New("network error"))
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	result := detector.CheckInterrupt(context.Background(), "hello")

	// On error, should fail safe (assume interrupt)
	if !result.IsInterrupt {
		t.Error("on error, should assume interrupt")
	}
	if result.Reason != "error_fallback" {
		t.Errorf("expected reason 'error_fallback', got %q", result.Reason)
	}
}

func TestInterruptDetector_CustomModel(t *testing.T) {
	var capturedModel string
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		capturedModel = req.Model
		return LLMResponse{Text: "NO"}
	}

	detector := NewInterruptDetector(llmFunc, InterruptConfig{
		SemanticModel: "custom/interrupt-model",
	})

	detector.CheckInterrupt(context.Background(), "hello")

	if capturedModel != "custom/interrupt-model" {
		t.Errorf("expected model 'custom/interrupt-model', got %q", capturedModel)
	}
}

func TestInterruptDetector_PromptFormat(t *testing.T) {
	var capturedPrompt string
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		capturedPrompt = req.Prompt
		return LLMResponse{Text: "NO"}
	}

	detector := NewInterruptDetector(llmFunc, InterruptConfig{})
	detector.CheckInterrupt(context.Background(), "actually wait")

	if !strings.Contains(capturedPrompt, "actually wait") {
		t.Error("prompt should contain the transcript")
	}
	if !strings.Contains(capturedPrompt, "assistant is currently speaking") {
		t.Error("prompt should provide context about assistant speaking")
	}
}

func TestInterruptDetector_Metrics(t *testing.T) {
	// Use a stateful mock that returns YES for "stop" and NO otherwise
	callCount := 0
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		callCount++
		if strings.Contains(req.Prompt, `user said: "stop"`) {
			return LLMResponse{Text: "YES"}
		}
		return LLMResponse{Text: "NO"}
	}
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	// Mix of interrupts and backchannels
	detector.CheckInterrupt(context.Background(), "stop")
	detector.CheckInterrupt(context.Background(), "uh huh")
	detector.CheckInterrupt(context.Background(), "mm hmm")

	metrics := detector.Metrics()
	if metrics.TotalCalls != 3 {
		t.Errorf("expected 3 calls, got %d", metrics.TotalCalls)
	}
	if metrics.Interrupts != 1 {
		t.Errorf("expected 1 interrupt, got %d", metrics.Interrupts)
	}
	if metrics.Backchannels != 2 {
		t.Errorf("expected 2 backchannels, got %d", metrics.Backchannels)
	}
}

func TestInterruptDetector_UpdateConfig(t *testing.T) {
	detector := NewInterruptDetector(nil, InterruptConfig{
		Mode: InterruptModeAuto,
	})

	detector.UpdateConfig(InterruptConfig{
		Mode:          InterruptModeManual,
		SemanticModel: "new/model",
	})

	// Verify config was updated (internal state)
	// We can't directly access config, but we can test behavior
	// For now, just ensure no panic
}

func TestInterruptDetector_ResetMetrics(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 0)
	detector := NewInterruptDetector(llmFunc, InterruptConfig{})

	detector.CheckInterrupt(context.Background(), "hello")
	detector.CheckInterrupt(context.Background(), "stop")

	metrics := detector.Metrics()
	if metrics.TotalCalls != 2 {
		t.Errorf("expected 2 calls, got %d", metrics.TotalCalls)
	}

	detector.ResetMetrics()
	metrics = detector.Metrics()
	if metrics.TotalCalls != 0 {
		t.Errorf("expected 0 calls after reset, got %d", metrics.TotalCalls)
	}
}

// --- Integration with HybridVAD Tests ---

func TestHybridVAD_WithLLMSemanticChecker(t *testing.T) {
	// Create a mock LLM that detects complete vs incomplete sentences
	llmFunc := func(ctx context.Context, req LLMRequest) LLMResponse {
		// Check if the transcript ends with "please" (complete)
		if strings.Contains(req.Prompt, "please") {
			return LLMResponse{Text: "YES", LatencyMs: 10}
		}
		// Otherwise incomplete
		return LLMResponse{Text: "NO", LatencyMs: 10}
	}

	checker := NewLLMSemanticChecker(llmFunc, LLMSemanticCheckerConfig{})

	semanticCheck := true
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  2,
	}, checker)

	// Test incomplete sentence
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "Book me a flight to")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)
	result := vad.ProcessAudio(silence, "")

	if result != VADContinue {
		t.Error("incomplete sentence should return VADContinue")
	}

	// Reset and test complete sentence
	vad.Reset()
	vad.ProcessAudio(speech, "Book me a flight to Paris please")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)
	result = vad.ProcessAudio(silence, "")

	if result != VADCommit {
		t.Error("complete sentence should return VADCommit")
	}
}

// --- Mock LLM Function Tests ---

func TestMockLLMFunc_DefaultResponse(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "DEFAULT", 0)
	resp := llmFunc(context.Background(), LLMRequest{Prompt: "test"})

	if resp.Text != "DEFAULT" {
		t.Errorf("expected 'DEFAULT', got %q", resp.Text)
	}
}

func TestMockLLMFunc_MatchingResponse(t *testing.T) {
	responses := map[string]string{
		"hello": "HELLO_RESPONSE",
		"world": "WORLD_RESPONSE",
	}
	llmFunc := MockLLMFunc(responses, "DEFAULT", 0)

	resp := llmFunc(context.Background(), LLMRequest{Prompt: "Say hello"})
	if resp.Text != "HELLO_RESPONSE" {
		t.Errorf("expected 'HELLO_RESPONSE', got %q", resp.Text)
	}

	resp = llmFunc(context.Background(), LLMRequest{Prompt: "Hello world"})
	// Should match first found - order not guaranteed in maps
	if resp.Text != "HELLO_RESPONSE" && resp.Text != "WORLD_RESPONSE" {
		t.Errorf("expected matching response, got %q", resp.Text)
	}
}

func TestMockLLMFunc_Latency(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 50*time.Millisecond)

	start := time.Now()
	llmFunc(context.Background(), LLMRequest{Prompt: "test"})
	elapsed := time.Since(start)

	if elapsed < 40*time.Millisecond {
		t.Errorf("expected at least 40ms latency, got %v", elapsed)
	}
}

func TestMockLLMFunc_ContextCancellation(t *testing.T) {
	llmFunc := MockLLMFunc(nil, "YES", 200*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	resp := llmFunc(ctx, LLMRequest{Prompt: "test"})
	elapsed := time.Since(start)

	if resp.Error == nil {
		t.Error("expected context cancellation error")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("should have cancelled quickly, took %v", elapsed)
	}
}

func TestErrorLLMFunc(t *testing.T) {
	expectedErr := errors.New("test error")
	llmFunc := ErrorLLMFunc(expectedErr)

	resp := llmFunc(context.Background(), LLMRequest{Prompt: "test"})

	if resp.Error != expectedErr {
		t.Errorf("expected %v, got %v", expectedErr, resp.Error)
	}
}

// --- Prompt Constant Tests ---

func TestTurnCompletionPrompt(t *testing.T) {
	if !strings.Contains(TurnCompletionPrompt, "Voice transcript") {
		t.Error("prompt should reference voice transcript")
	}
	if !strings.Contains(TurnCompletionPrompt, "YES or NO") {
		t.Error("prompt should ask for YES/NO")
	}
	if !strings.Contains(TurnCompletionPrompt, "trailing conjunctions") {
		t.Error("prompt should mention trailing conjunctions")
	}
}

func TestInterruptClassificationPrompt(t *testing.T) {
	if !strings.Contains(InterruptClassificationPrompt, "assistant is currently speaking") {
		t.Error("prompt should mention assistant speaking")
	}
	if !strings.Contains(InterruptClassificationPrompt, "backchannel") {
		t.Error("prompt should mention backchannels")
	}
	if !strings.Contains(InterruptClassificationPrompt, "YES or NO") {
		t.Error("prompt should ask for YES/NO")
	}
}

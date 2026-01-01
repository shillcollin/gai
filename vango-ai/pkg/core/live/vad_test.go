package live

import (
	"sync/atomic"
	"testing"
	"time"
)

// mockSemanticChecker is a mock implementation for testing.
type mockSemanticChecker struct {
	response   bool
	err        error
	callCount  atomic.Int32
	lastText   string
	delayMs    int
}

func (m *mockSemanticChecker) CheckTurnComplete(transcript string) (bool, error) {
	m.callCount.Add(1)
	m.lastText = transcript
	if m.delayMs > 0 {
		time.Sleep(time.Duration(m.delayMs) * time.Millisecond)
	}
	return m.response, m.err
}

func TestNewHybridVAD_DefaultConfig(t *testing.T) {
	vad := NewHybridVAD(VADConfig{})

	cfg := vad.Config()
	if cfg.EnergyThreshold != 0.02 {
		t.Errorf("expected default EnergyThreshold 0.02, got %f", cfg.EnergyThreshold)
	}
	if cfg.SilenceDurationMs != 600 {
		t.Errorf("expected default SilenceDurationMs 600, got %d", cfg.SilenceDurationMs)
	}
	if cfg.MaxSilenceMs != 3000 {
		t.Errorf("expected default MaxSilenceMs 3000, got %d", cfg.MaxSilenceMs)
	}
	if cfg.MinWordsForCheck != 2 {
		t.Errorf("expected default MinWordsForCheck 2, got %d", cfg.MinWordsForCheck)
	}
}

func TestNewHybridVAD_CustomConfig(t *testing.T) {
	semanticCheck := false
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.05,
		SilenceDurationMs: 400,
		MaxSilenceMs:      2000,
		SemanticCheck:     &semanticCheck,
	})

	cfg := vad.Config()
	if cfg.EnergyThreshold != 0.05 {
		t.Errorf("expected EnergyThreshold 0.05, got %f", cfg.EnergyThreshold)
	}
	if cfg.SilenceDurationMs != 400 {
		t.Errorf("expected SilenceDurationMs 400, got %d", cfg.SilenceDurationMs)
	}
	if cfg.MaxSilenceMs != 2000 {
		t.Errorf("expected MaxSilenceMs 2000, got %d", cfg.MaxSilenceMs)
	}
}

func TestHybridVAD_InitialState(t *testing.T) {
	vad := NewHybridVAD(VADConfig{})

	if vad.State() != VADStateListening {
		t.Errorf("expected initial state Listening, got %s", vad.State())
	}
	if vad.GetTranscript() != "" {
		t.Errorf("expected empty transcript, got %q", vad.GetTranscript())
	}
	if vad.LastEnergy() != 0 {
		t.Errorf("expected LastEnergy 0, got %f", vad.LastEnergy())
	}
}

func TestHybridVAD_SilenceRemainsContinue(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 600,
	})

	silence := generateSilence(1000)
	result := vad.ProcessAudio(silence, "")

	if result != VADContinue {
		t.Errorf("expected VADContinue for silence-only input, got %v", result)
	}
	if vad.State() != VADStateListening {
		t.Errorf("expected state Listening, got %s", vad.State())
	}
}

func TestHybridVAD_SpeechTransition(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	// Start with silence
	silence := generateSilence(1000)
	vad.ProcessAudio(silence, "")
	if vad.State() != VADStateListening {
		t.Errorf("expected state Listening, got %s", vad.State())
	}

	// Send speech
	speech := generatePCM(0.1, 1000)
	result := vad.ProcessAudio(speech, "hello")

	if result != VADContinue {
		t.Errorf("expected VADContinue during speech, got %v", result)
	}
	if vad.State() != VADStateSpeech {
		t.Errorf("expected state Speech, got %s", vad.State())
	}
}

func TestHybridVAD_TranscriptAccumulation(t *testing.T) {
	vad := NewHybridVAD(VADConfig{})

	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello ")
	vad.ProcessAudio(speech, "world")

	transcript := vad.GetTranscript()
	if transcript != "hello world" {
		t.Errorf("expected transcript 'hello world', got %q", transcript)
	}
}

func TestHybridVAD_SilenceAfterSpeech(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	// Send speech first
	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello")
	if vad.State() != VADStateSpeech {
		t.Errorf("expected state Speech, got %s", vad.State())
	}

	// Send silence
	silence := generateSilence(1000)
	vad.ProcessAudio(silence, "")

	if vad.State() != VADStateSilence {
		t.Errorf("expected state Silence, got %s", vad.State())
	}
}

func TestHybridVAD_SpeechResumption(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 600,
	})

	// Speech -> Silence -> Speech
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hello")
	vad.ProcessAudio(silence, "")
	if vad.State() != VADStateSilence {
		t.Errorf("expected state Silence, got %s", vad.State())
	}

	// Speech resumes
	vad.ProcessAudio(speech, " world")
	if vad.State() != VADStateSpeech {
		t.Errorf("expected state Speech after resumption, got %s", vad.State())
	}
}

func TestHybridVAD_CommitAfterSilenceThreshold(t *testing.T) {
	semanticCheck := false
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 100, // Short threshold for testing
		MaxSilenceMs:      3000,
		SemanticCheck:     &semanticCheck,
	})

	// Send speech
	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello")

	// Send silence and wait
	silence := generateSilence(1000)
	vad.ProcessAudio(silence, "")
	time.Sleep(150 * time.Millisecond)

	// Process another silence chunk - should trigger commit
	result := vad.ProcessAudio(silence, "")

	if result != VADCommit {
		t.Errorf("expected VADCommit after silence threshold, got %v", result)
	}
}

func TestHybridVAD_ForceCommitAfterMaxSilence(t *testing.T) {
	semanticCheck := true
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 100,
		MaxSilenceMs:      200, // Short max for testing
		SemanticCheck:     &semanticCheck,
	})

	// Send speech
	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello")

	// Send silence and wait past max
	silence := generateSilence(1000)
	vad.ProcessAudio(silence, "")
	time.Sleep(250 * time.Millisecond)

	// Should force commit regardless of semantic check
	result := vad.ProcessAudio(silence, "")

	if result != VADCommit {
		t.Errorf("expected VADCommit after max silence, got %v", result)
	}
}

func TestHybridVAD_Reset(t *testing.T) {
	vad := NewHybridVAD(VADConfig{})

	// Build up some state
	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello world")

	if vad.GetTranscript() == "" {
		t.Error("expected transcript before reset")
	}

	vad.Reset()

	if vad.State() != VADStateListening {
		t.Errorf("expected state Listening after reset, got %s", vad.State())
	}
	if vad.GetTranscript() != "" {
		t.Errorf("expected empty transcript after reset, got %q", vad.GetTranscript())
	}
	if vad.LastEnergy() != 0 {
		t.Errorf("expected LastEnergy 0 after reset, got %f", vad.LastEnergy())
	}
}

func TestHybridVAD_AddTranscript(t *testing.T) {
	vad := NewHybridVAD(VADConfig{})

	vad.AddTranscript("pre-existing text ")

	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "more text")

	transcript := vad.GetTranscript()
	if transcript != "pre-existing text more text" {
		t.Errorf("expected combined transcript, got %q", transcript)
	}
}

func TestHybridVAD_UpdateConfig(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	vad.UpdateConfig(VADConfig{
		EnergyThreshold: 0.05,
	})

	cfg := vad.Config()
	if cfg.EnergyThreshold != 0.05 {
		t.Errorf("expected updated EnergyThreshold 0.05, got %f", cfg.EnergyThreshold)
	}
}

func TestHybridVAD_StateChangeCallback(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	var stateChanges []VADState
	vad.SetOnStateChange(func(state VADState) {
		stateChanges = append(stateChanges, state)
	})

	// Trigger state changes
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hello")
	time.Sleep(10 * time.Millisecond) // Allow async callback
	vad.ProcessAudio(silence, "")
	time.Sleep(10 * time.Millisecond)

	if len(stateChanges) < 2 {
		t.Errorf("expected at least 2 state changes, got %d", len(stateChanges))
	}
}

func TestHybridVAD_SemanticChecker_NotCalled_WhenDisabled(t *testing.T) {
	semanticCheck := false
	mock := &mockSemanticChecker{response: true}
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		SemanticCheck:     &semanticCheck,
	}, mock)

	// Send speech then silence
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hello")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)
	vad.ProcessAudio(silence, "")

	if mock.callCount.Load() != 0 {
		t.Errorf("expected semantic checker not called when disabled, got %d calls", mock.callCount.Load())
	}
}

func TestHybridVAD_SemanticChecker_Called_WhenEnabled(t *testing.T) {
	semanticCheck := true
	mock := &mockSemanticChecker{response: true}
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  1,
	}, mock)

	// Send speech then silence
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hello world")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)
	vad.ProcessAudio(silence, "")

	if mock.callCount.Load() == 0 {
		t.Error("expected semantic checker to be called")
	}
	if mock.lastText != "hello world" {
		t.Errorf("expected lastText 'hello world', got %q", mock.lastText)
	}
}

func TestHybridVAD_SemanticChecker_SkippedForShortUtterances(t *testing.T) {
	semanticCheck := true
	mock := &mockSemanticChecker{response: true}
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  3, // Require 3+ words
	}, mock)

	// Send short utterance
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hi") // Only 1 word
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)
	vad.ProcessAudio(silence, "")

	if mock.callCount.Load() != 0 {
		t.Errorf("expected semantic checker skipped for short utterance, got %d calls", mock.callCount.Load())
	}
}

func TestHybridVAD_SemanticChecker_Continue_WhenNo(t *testing.T) {
	semanticCheck := true
	mock := &mockSemanticChecker{response: false} // Not complete
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		MaxSilenceMs:      5000, // Long max so we don't force commit
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  1,
	}, mock)

	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "book me a flight to")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)

	// Should check semantics and return continue
	result := vad.ProcessAudio(silence, "")

	if result != VADContinue {
		t.Errorf("expected VADContinue when semantic says incomplete, got %v", result)
	}
}

func TestHybridVAD_SemanticChecker_Commit_WhenYes(t *testing.T) {
	semanticCheck := true
	mock := &mockSemanticChecker{response: true} // Complete
	vad := NewHybridVADWithSemantics(VADConfig{
		EnergyThreshold:   0.02,
		SilenceDurationMs: 50,
		SemanticCheck:     &semanticCheck,
		MinWordsForCheck:  1,
	}, mock)

	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "book me a flight to paris please")
	vad.ProcessAudio(silence, "")
	time.Sleep(100 * time.Millisecond)

	result := vad.ProcessAudio(silence, "")

	if result != VADCommit {
		t.Errorf("expected VADCommit when semantic says complete, got %v", result)
	}
}

func TestHybridVAD_SilenceDuration(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	// Initially no silence
	if vad.SilenceDuration() != 0 {
		t.Error("expected SilenceDuration=0 initially")
	}

	// Send speech then silence
	speech := generatePCM(0.1, 1000)
	silence := generateSilence(1000)

	vad.ProcessAudio(speech, "hello")
	vad.ProcessAudio(silence, "")

	time.Sleep(100 * time.Millisecond)

	dur := vad.SilenceDuration()
	if dur < 90*time.Millisecond {
		t.Errorf("expected SilenceDuration >= 90ms, got %v", dur)
	}
}

func TestHybridVAD_SpeechDuration(t *testing.T) {
	vad := NewHybridVAD(VADConfig{
		EnergyThreshold: 0.02,
	})

	// Initially no speech
	if vad.SpeechDuration() != 0 {
		t.Error("expected SpeechDuration=0 initially")
	}

	// Send speech
	speech := generatePCM(0.1, 1000)
	vad.ProcessAudio(speech, "hello")

	time.Sleep(100 * time.Millisecond)

	dur := vad.SpeechDuration()
	if dur < 90*time.Millisecond {
		t.Errorf("expected SpeechDuration >= 90ms, got %v", dur)
	}
}

func TestVADState_String(t *testing.T) {
	tests := []struct {
		state    VADState
		expected string
	}{
		{VADStateListening, "listening"},
		{VADStateSpeech, "speech"},
		{VADStateSilence, "silence"},
		{VADStateAnalyzing, "analyzing"},
		{VADState(99), "unknown"},
	}

	for _, tc := range tests {
		if tc.state.String() != tc.expected {
			t.Errorf("expected %s.String() = %q, got %q", tc.expected, tc.expected, tc.state.String())
		}
	}
}

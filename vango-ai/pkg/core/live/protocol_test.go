package live

import (
	"testing"
)

func TestDefaultVADConfig(t *testing.T) {
	cfg := DefaultVADConfig()

	if cfg.Model != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("expected default model 'anthropic/claude-haiku-4-5-20251001', got %q", cfg.Model)
	}
	if cfg.EnergyThreshold != 0.02 {
		t.Errorf("expected EnergyThreshold 0.02, got %f", cfg.EnergyThreshold)
	}
	if cfg.SilenceDurationMs != 600 {
		t.Errorf("expected SilenceDurationMs 600, got %d", cfg.SilenceDurationMs)
	}
	if cfg.SemanticCheck == nil || *cfg.SemanticCheck != true {
		t.Error("expected SemanticCheck true")
	}
	if cfg.MinWordsForCheck != 2 {
		t.Errorf("expected MinWordsForCheck 2, got %d", cfg.MinWordsForCheck)
	}
	if cfg.MaxSilenceMs != 3000 {
		t.Errorf("expected MaxSilenceMs 3000, got %d", cfg.MaxSilenceMs)
	}
}

func TestDefaultInterruptConfig(t *testing.T) {
	cfg := DefaultInterruptConfig()

	if cfg.Mode != InterruptModeAuto {
		t.Errorf("expected Mode 'auto', got %q", cfg.Mode)
	}
	if cfg.EnergyThreshold != 0.05 {
		t.Errorf("expected EnergyThreshold 0.05, got %f", cfg.EnergyThreshold)
	}
	if cfg.DebounceMs != 100 {
		t.Errorf("expected DebounceMs 100, got %d", cfg.DebounceMs)
	}
	if cfg.SemanticCheck == nil || *cfg.SemanticCheck != true {
		t.Error("expected SemanticCheck true")
	}
	if cfg.SemanticModel != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("expected SemanticModel 'anthropic/claude-haiku-4-5-20251001', got %q", cfg.SemanticModel)
	}
	if cfg.SavePartial != SaveBehaviorMarked {
		t.Errorf("expected SavePartial 'marked', got %q", cfg.SavePartial)
	}
}

func TestSessionConfig_ApplyDefaults_NilVoice(t *testing.T) {
	cfg := &SessionConfig{
		Model: "test-model",
	}

	cfg.ApplyDefaults()

	if cfg.Voice == nil {
		t.Fatal("expected Voice to be set")
	}
	if cfg.Voice.VAD == nil {
		t.Fatal("expected Voice.VAD to be set")
	}
	if cfg.Voice.Interrupt == nil {
		t.Fatal("expected Voice.Interrupt to be set")
	}

	// Check VAD defaults applied
	if cfg.Voice.VAD.EnergyThreshold != 0.02 {
		t.Errorf("expected VAD.EnergyThreshold 0.02, got %f", cfg.Voice.VAD.EnergyThreshold)
	}

	// Check Interrupt defaults applied
	if cfg.Voice.Interrupt.Mode != InterruptModeAuto {
		t.Errorf("expected Interrupt.Mode 'auto', got %q", cfg.Voice.Interrupt.Mode)
	}
}

func TestSessionConfig_ApplyDefaults_PartialVAD(t *testing.T) {
	semanticCheck := false
	cfg := &SessionConfig{
		Model: "test-model",
		Voice: &VoiceConfig{
			VAD: &VADConfig{
				EnergyThreshold: 0.05, // Custom value
				SemanticCheck:   &semanticCheck,
			},
		},
	}

	cfg.ApplyDefaults()

	// Custom value should be preserved
	if cfg.Voice.VAD.EnergyThreshold != 0.05 {
		t.Errorf("expected custom EnergyThreshold 0.05, got %f", cfg.Voice.VAD.EnergyThreshold)
	}

	// Default values should be applied to unset fields
	if cfg.Voice.VAD.SilenceDurationMs != 600 {
		t.Errorf("expected default SilenceDurationMs 600, got %d", cfg.Voice.VAD.SilenceDurationMs)
	}

	// Custom semantic check should be preserved
	if *cfg.Voice.VAD.SemanticCheck != false {
		t.Error("expected custom SemanticCheck false to be preserved")
	}
}

func TestSessionConfig_ApplyDefaults_PartialInterrupt(t *testing.T) {
	cfg := &SessionConfig{
		Model: "test-model",
		Voice: &VoiceConfig{
			Interrupt: &InterruptConfig{
				Mode:        InterruptModeManual, // Custom value
				SavePartial: SaveBehaviorDiscard,
			},
		},
	}

	cfg.ApplyDefaults()

	// Custom value should be preserved
	if cfg.Voice.Interrupt.Mode != InterruptModeManual {
		t.Errorf("expected custom Mode 'manual', got %q", cfg.Voice.Interrupt.Mode)
	}
	if cfg.Voice.Interrupt.SavePartial != SaveBehaviorDiscard {
		t.Errorf("expected custom SavePartial 'discard', got %q", cfg.Voice.Interrupt.SavePartial)
	}

	// Default values should be applied to unset fields
	if cfg.Voice.Interrupt.EnergyThreshold != 0.05 {
		t.Errorf("expected default EnergyThreshold 0.05, got %f", cfg.Voice.Interrupt.EnergyThreshold)
	}
	if cfg.Voice.Interrupt.SemanticModel != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("expected default SemanticModel, got %q", cfg.Voice.Interrupt.SemanticModel)
	}
}

func TestInterruptMode_Constants(t *testing.T) {
	if InterruptModeAuto != "auto" {
		t.Errorf("expected InterruptModeAuto = 'auto', got %q", InterruptModeAuto)
	}
	if InterruptModeManual != "manual" {
		t.Errorf("expected InterruptModeManual = 'manual', got %q", InterruptModeManual)
	}
	if InterruptModeDisabled != "disabled" {
		t.Errorf("expected InterruptModeDisabled = 'disabled', got %q", InterruptModeDisabled)
	}
}

func TestSaveBehavior_Constants(t *testing.T) {
	if SaveBehaviorDiscard != "discard" {
		t.Errorf("expected SaveBehaviorDiscard = 'discard', got %q", SaveBehaviorDiscard)
	}
	if SaveBehaviorSave != "save" {
		t.Errorf("expected SaveBehaviorSave = 'save', got %q", SaveBehaviorSave)
	}
	if SaveBehaviorMarked != "marked" {
		t.Errorf("expected SaveBehaviorMarked = 'marked', got %q", SaveBehaviorMarked)
	}
}

func TestEventTypes(t *testing.T) {
	// Verify event type constants
	tests := []struct {
		constant string
		expected string
	}{
		{EventTypeSessionConfigure, "session.configure"},
		{EventTypeSessionUpdate, "session.update"},
		{EventTypeInputInterrupt, "input.interrupt"},
		{EventTypeInputCommit, "input.commit"},
		{EventTypeInputText, "input.text"},
		{EventTypeSessionCreated, "session.created"},
		{EventTypeVADListening, "vad.listening"},
		{EventTypeVADAnalyzing, "vad.analyzing"},
		{EventTypeVADSilence, "vad.silence"},
		{EventTypeInputCommitted, "input.committed"},
		{EventTypeTranscriptDelta, "transcript.delta"},
		{EventTypeAudioDelta, "audio_delta"},
		{EventTypeInterruptDetecting, "interrupt.detecting"},
		{EventTypeInterruptDismissed, "interrupt.dismissed"},
		{EventTypeResponseInterrupted, "response.interrupted"},
		{EventTypeMessageStop, "message_stop"},
		{EventTypeError, "error"},
	}

	for _, tc := range tests {
		if tc.constant != tc.expected {
			t.Errorf("expected %q, got %q", tc.expected, tc.constant)
		}
	}
}

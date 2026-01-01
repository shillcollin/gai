package live

import (
	"strings"
	"sync"
	"time"
)

// VADResult indicates the outcome of VAD processing.
type VADResult int

const (
	// VADContinue means keep buffering, turn not complete.
	VADContinue VADResult = iota

	// VADCommit means turn is complete, trigger agent.
	VADCommit

	// VADPendingSemantic means energy check passed, waiting for semantic check.
	// This is used when semantic checking is enabled.
	VADPendingSemantic
)

// VADState represents the current state of the VAD.
type VADState int

const (
	// VADStateListening indicates actively listening for speech.
	VADStateListening VADState = iota

	// VADStateSpeech indicates speech has been detected.
	VADStateSpeech

	// VADStateSilence indicates silence detected after speech.
	VADStateSilence

	// VADStateAnalyzing indicates semantic analysis in progress.
	VADStateAnalyzing
)

// String returns a string representation of the VAD state.
func (s VADState) String() string {
	switch s {
	case VADStateListening:
		return "listening"
	case VADStateSpeech:
		return "speech"
	case VADStateSilence:
		return "silence"
	case VADStateAnalyzing:
		return "analyzing"
	default:
		return "unknown"
	}
}

// SemanticChecker is an interface for semantic turn completion checking.
// This will be implemented in Phase 7.2.
type SemanticChecker interface {
	// CheckTurnComplete checks if the transcript represents a complete turn.
	// Returns true if the speaker is done, false if they might continue.
	CheckTurnComplete(transcript string) (bool, error)
}

// HybridVAD implements hybrid voice activity detection combining
// energy-based silence detection with optional semantic analysis.
type HybridVAD struct {
	config VADConfig

	// Optional semantic checker (Phase 7.2)
	semanticChecker SemanticChecker

	// State
	mu           sync.Mutex
	state        VADState
	transcript   strings.Builder
	silenceStart time.Time
	speechStart  time.Time

	// Energy tracking
	lastEnergy float64

	// Callback for state changes
	onStateChange func(VADState)
}

// NewHybridVAD creates a new hybrid VAD with the given configuration.
func NewHybridVAD(config VADConfig) *HybridVAD {
	// Apply defaults
	if config.EnergyThreshold == 0 {
		config.EnergyThreshold = DefaultVADConfig().EnergyThreshold
	}
	if config.SilenceDurationMs == 0 {
		config.SilenceDurationMs = DefaultVADConfig().SilenceDurationMs
	}
	if config.MaxSilenceMs == 0 {
		config.MaxSilenceMs = DefaultVADConfig().MaxSilenceMs
	}
	if config.MinWordsForCheck == 0 {
		config.MinWordsForCheck = DefaultVADConfig().MinWordsForCheck
	}
	if config.SemanticCheck == nil {
		config.SemanticCheck = DefaultVADConfig().SemanticCheck
	}

	return &HybridVAD{
		config: config,
		state:  VADStateListening,
	}
}

// NewHybridVADWithSemantics creates a VAD with a semantic checker.
func NewHybridVADWithSemantics(config VADConfig, checker SemanticChecker) *HybridVAD {
	vad := NewHybridVAD(config)
	vad.semanticChecker = checker
	return vad
}

// SetSemanticChecker sets the semantic checker for turn completion.
func (v *HybridVAD) SetSemanticChecker(checker SemanticChecker) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.semanticChecker = checker
}

// SetOnStateChange sets a callback for state changes.
func (v *HybridVAD) SetOnStateChange(fn func(VADState)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onStateChange = fn
}

// ProcessAudio processes an audio chunk and updates VAD state.
// transcriptDelta is the incremental transcript from STT (if available).
// Returns the VAD result indicating whether to continue or commit.
func (v *HybridVAD) ProcessAudio(chunk []byte, transcriptDelta string) VADResult {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()

	// Accumulate transcript
	if transcriptDelta != "" {
		v.transcript.WriteString(transcriptDelta)
	}

	// Calculate energy
	energy := calculateRMSEnergy(chunk)
	v.lastEnergy = energy

	// Stage 1: Energy-based detection
	isSpeech := energy >= v.config.EnergyThreshold

	switch v.state {
	case VADStateListening:
		if isSpeech {
			// Speech started
			v.speechStart = now
			v.setState(VADStateSpeech)
		}
		return VADContinue

	case VADStateSpeech:
		if !isSpeech {
			// Silence started
			v.silenceStart = now
			v.setState(VADStateSilence)
		}
		return VADContinue

	case VADStateSilence:
		if isSpeech {
			// Speech resumed, cancel silence timer
			v.setState(VADStateSpeech)
			return VADContinue
		}

		silenceDuration := now.Sub(v.silenceStart)

		// Force commit after max silence (prevent infinite wait)
		maxSilence := time.Duration(v.config.MaxSilenceMs) * time.Millisecond
		if silenceDuration >= maxSilence {
			return VADCommit
		}

		// Check if we've hit the silence threshold
		silenceThreshold := time.Duration(v.config.SilenceDurationMs) * time.Millisecond
		if silenceDuration >= silenceThreshold {
			// In Phase 7.1, we do energy-only detection
			// Semantic check will be added in Phase 7.2
			if v.shouldDoSemanticCheck() {
				return v.doSemanticCheck()
			}
			return VADCommit
		}

		return VADContinue

	case VADStateAnalyzing:
		// Semantic check in progress (Phase 7.2)
		// For now, just return continue
		return VADContinue
	}

	return VADContinue
}

// shouldDoSemanticCheck returns true if semantic checking should be performed.
func (v *HybridVAD) shouldDoSemanticCheck() bool {
	// Skip if semantic check disabled
	if v.config.SemanticCheck != nil && !*v.config.SemanticCheck {
		return false
	}

	// Skip if no semantic checker configured
	if v.semanticChecker == nil {
		return false
	}

	// Skip for very short utterances
	text := v.transcript.String()
	words := strings.Fields(text)
	if len(words) < v.config.MinWordsForCheck {
		return false
	}

	return true
}

// doSemanticCheck performs the semantic turn completion check.
func (v *HybridVAD) doSemanticCheck() VADResult {
	if v.semanticChecker == nil {
		return VADCommit
	}

	v.setState(VADStateAnalyzing)

	text := v.transcript.String()
	complete, err := v.semanticChecker.CheckTurnComplete(text)
	if err != nil {
		// On error, fail open (commit the turn)
		return VADCommit
	}

	if complete {
		return VADCommit
	}

	// Not complete, extend silence window and keep listening
	v.silenceStart = time.Now()
	v.setState(VADStateSilence)
	return VADContinue
}

// setState updates the VAD state and triggers callback.
func (v *HybridVAD) setState(state VADState) {
	if v.state != state {
		v.state = state
		if v.onStateChange != nil {
			// Call async to avoid holding lock
			fn := v.onStateChange
			go fn(state)
		}
	}
}

// Reset resets the VAD state for a new turn.
func (v *HybridVAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.transcript.Reset()
	v.state = VADStateListening
	v.silenceStart = time.Time{}
	v.speechStart = time.Time{}
	v.lastEnergy = 0
}

// GetTranscript returns the accumulated transcript.
func (v *HybridVAD) GetTranscript() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.transcript.String()
}

// AddTranscript adds text to the transcript (used when recovering from interrupt).
func (v *HybridVAD) AddTranscript(text string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.transcript.WriteString(text)
}

// State returns the current VAD state.
func (v *HybridVAD) State() VADState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state
}

// LastEnergy returns the most recent energy level.
func (v *HybridVAD) LastEnergy() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastEnergy
}

// SilenceDuration returns how long the VAD has been in silence state.
// Returns 0 if not in silence state.
func (v *HybridVAD) SilenceDuration() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.state != VADStateSilence {
		return 0
	}
	return time.Since(v.silenceStart)
}

// SpeechDuration returns how long since speech was first detected.
// Returns 0 if no speech detected yet.
func (v *HybridVAD) SpeechDuration() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.speechStart.IsZero() {
		return 0
	}
	return time.Since(v.speechStart)
}

// Config returns the VAD configuration.
func (v *HybridVAD) Config() VADConfig {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.config
}

// UpdateConfig updates the VAD configuration.
func (v *HybridVAD) UpdateConfig(config VADConfig) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if config.Model != "" {
		v.config.Model = config.Model
	}
	if config.EnergyThreshold != 0 {
		v.config.EnergyThreshold = config.EnergyThreshold
	}
	if config.SilenceDurationMs != 0 {
		v.config.SilenceDurationMs = config.SilenceDurationMs
	}
	if config.SemanticCheck != nil {
		v.config.SemanticCheck = config.SemanticCheck
	}
	if config.MinWordsForCheck != 0 {
		v.config.MinWordsForCheck = config.MinWordsForCheck
	}
	if config.MaxSilenceMs != 0 {
		v.config.MaxSilenceMs = config.MaxSilenceMs
	}
}

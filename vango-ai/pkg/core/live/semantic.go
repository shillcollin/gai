package live

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// --- Prompts ---

// TurnCompletionPrompt is the prompt used for VAD semantic turn completion detection.
const TurnCompletionPrompt = `Voice transcript: "%s"

Is the speaker clearly done and waiting for a response? Consider: trailing conjunctions (and, but, so), incomplete thoughts, rhetorical pauses, or filler words.

Reply only: YES or NO`

// InterruptClassificationPrompt is the prompt used for interrupt detection.
const InterruptClassificationPrompt = `The AI assistant is currently speaking. The user said: "%s"

Is the user trying to interrupt and take over the conversation?

Answer YES if: stopping the assistant, changing topic, asking a new question, correcting, disagreeing, saying "wait", "stop", "actually", "no", "hold on"

Answer NO if: backchannel acknowledgment (uh huh, mm hmm, right, okay, yeah, got it), thinking sounds (um, uh), brief encouragement to continue

Reply only: YES or NO`

// --- Types ---

// LLMRequest represents a request to an LLM.
type LLMRequest struct {
	Model       string  `json:"model"`
	Prompt      string  `json:"prompt"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

// LLMResponse represents a response from an LLM.
type LLMResponse struct {
	Text      string   `json:"text"`
	LatencyMs int64    `json:"latency_ms"`
	Error     error    `json:"-"`
	Model     string   `json:"model"`
	Usage     LLMUsage `json:"usage,omitempty"`
}

// LLMUsage contains token usage information.
type LLMUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// LLMFunc is a function that executes an LLM request.
// This abstraction allows the semantic checker to work with any LLM client.
type LLMFunc func(ctx context.Context, req LLMRequest) LLMResponse

// --- LLM Semantic Checker ---

// LLMSemanticChecker implements SemanticChecker using an LLM.
type LLMSemanticChecker struct {
	llmFunc LLMFunc
	model   string
	timeout time.Duration

	// Metrics
	mu           sync.Mutex
	totalCalls   int64
	totalLatency time.Duration
	errors       int64
}

// LLMSemanticCheckerConfig configures the LLM semantic checker.
type LLMSemanticCheckerConfig struct {
	// Model is the LLM model to use (e.g., "anthropic/claude-haiku-4-5-20251001")
	Model string

	// Timeout for each LLM call (default: 500ms)
	Timeout time.Duration
}

// NewLLMSemanticChecker creates a new semantic checker using the provided LLM function.
func NewLLMSemanticChecker(llmFunc LLMFunc, config LLMSemanticCheckerConfig) *LLMSemanticChecker {
	if config.Model == "" {
		config.Model = "anthropic/claude-haiku-4-5-20251001"
	}
	if config.Timeout == 0 {
		config.Timeout = 500 * time.Millisecond
	}

	return &LLMSemanticChecker{
		llmFunc: llmFunc,
		model:   config.Model,
		timeout: config.Timeout,
	}
}

// CheckTurnComplete implements SemanticChecker.
// It calls the LLM to determine if the transcript represents a complete turn.
func (c *LLMSemanticChecker) CheckTurnComplete(transcript string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	start := time.Now()

	prompt := fmt.Sprintf(TurnCompletionPrompt, transcript)
	resp := c.llmFunc(ctx, LLMRequest{
		Model:       c.model,
		Prompt:      prompt,
		MaxTokens:   5,
		Temperature: 0.0,
	})

	latency := time.Since(start)

	// Update metrics
	c.mu.Lock()
	c.totalCalls++
	c.totalLatency += latency
	if resp.Error != nil {
		c.errors++
	}
	c.mu.Unlock()

	if resp.Error != nil {
		return false, resp.Error
	}

	// Parse response
	result := strings.ToUpper(strings.TrimSpace(resp.Text))
	return strings.Contains(result, "YES"), nil
}

// Metrics returns the checker's performance metrics.
func (c *LLMSemanticChecker) Metrics() SemanticCheckerMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()

	var avgLatency time.Duration
	if c.totalCalls > 0 {
		avgLatency = c.totalLatency / time.Duration(c.totalCalls)
	}

	return SemanticCheckerMetrics{
		TotalCalls:     c.totalCalls,
		TotalErrors:    c.errors,
		AverageLatency: avgLatency,
		Model:          c.model,
	}
}

// ResetMetrics resets the checker's metrics.
func (c *LLMSemanticChecker) ResetMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalCalls = 0
	c.totalLatency = 0
	c.errors = 0
}

// SemanticCheckerMetrics contains performance metrics.
type SemanticCheckerMetrics struct {
	TotalCalls     int64         `json:"total_calls"`
	TotalErrors    int64         `json:"total_errors"`
	AverageLatency time.Duration `json:"average_latency"`
	Model          string        `json:"model"`
}

// --- Interrupt Detector ---

// InterruptResult contains the result of interrupt detection.
type InterruptResult struct {
	IsInterrupt bool   `json:"is_interrupt"`
	Transcript  string `json:"transcript"`
	Reason      string `json:"reason"` // "interrupt", "backchannel", "noise", "too_short", "error_fallback"
	LatencyMs   int64  `json:"latency_ms"`
}

// InterruptDetector detects whether user speech during bot output is an interrupt.
type InterruptDetector struct {
	llmFunc LLMFunc
	config  InterruptConfig
	timeout time.Duration

	// Metrics
	mu           sync.Mutex
	totalCalls   int64
	totalLatency time.Duration
	interrupts   int64
	backchannels int64
	errors       int64
}

// NewInterruptDetector creates a new interrupt detector.
func NewInterruptDetector(llmFunc LLMFunc, config InterruptConfig) *InterruptDetector {
	timeout := 300 * time.Millisecond
	if config.DebounceMs > 0 {
		// Allow slightly more than debounce for the check
		timeout = time.Duration(config.DebounceMs+200) * time.Millisecond
	}

	return &InterruptDetector{
		llmFunc: llmFunc,
		config:  config,
		timeout: timeout,
	}
}

// CheckInterrupt determines if the transcript represents an interrupt.
func (d *InterruptDetector) CheckInterrupt(ctx context.Context, transcript string) InterruptResult {
	start := time.Now()

	// Skip check for very short sounds
	trimmed := strings.TrimSpace(transcript)
	if len(trimmed) < 2 {
		return InterruptResult{
			IsInterrupt: false,
			Transcript:  transcript,
			Reason:      "too_short",
			LatencyMs:   time.Since(start).Milliseconds(),
		}
	}

	// If semantic check is disabled, treat as interrupt
	if d.config.SemanticCheck != nil && !*d.config.SemanticCheck {
		return InterruptResult{
			IsInterrupt: true,
			Transcript:  transcript,
			Reason:      "semantic_disabled",
			LatencyMs:   time.Since(start).Milliseconds(),
		}
	}

	// Apply timeout
	checkCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Call LLM
	model := d.config.SemanticModel
	if model == "" {
		model = "anthropic/claude-haiku-4-5-20251001"
	}

	prompt := fmt.Sprintf(InterruptClassificationPrompt, transcript)
	resp := d.llmFunc(checkCtx, LLMRequest{
		Model:       model,
		Prompt:      prompt,
		MaxTokens:   5,
		Temperature: 0.0,
	})

	latency := time.Since(start)

	// Update metrics
	d.mu.Lock()
	d.totalCalls++
	d.totalLatency += latency
	if resp.Error != nil {
		d.errors++
	}
	d.mu.Unlock()

	if resp.Error != nil {
		// On error, assume interrupt (fail safe for user experience)
		return InterruptResult{
			IsInterrupt: true,
			Transcript:  transcript,
			Reason:      "error_fallback",
			LatencyMs:   latency.Milliseconds(),
		}
	}

	// Parse response
	result := strings.ToUpper(strings.TrimSpace(resp.Text))
	isInterrupt := strings.Contains(result, "YES")

	reason := "backchannel"
	if isInterrupt {
		reason = "interrupt"
		d.mu.Lock()
		d.interrupts++
		d.mu.Unlock()
	} else {
		d.mu.Lock()
		d.backchannels++
		d.mu.Unlock()
	}

	return InterruptResult{
		IsInterrupt: isInterrupt,
		Transcript:  transcript,
		Reason:      reason,
		LatencyMs:   latency.Milliseconds(),
	}
}

// Metrics returns the detector's performance metrics.
func (d *InterruptDetector) Metrics() InterruptDetectorMetrics {
	d.mu.Lock()
	defer d.mu.Unlock()

	var avgLatency time.Duration
	if d.totalCalls > 0 {
		avgLatency = d.totalLatency / time.Duration(d.totalCalls)
	}

	return InterruptDetectorMetrics{
		TotalCalls:     d.totalCalls,
		TotalErrors:    d.errors,
		Interrupts:     d.interrupts,
		Backchannels:   d.backchannels,
		AverageLatency: avgLatency,
	}
}

// ResetMetrics resets the detector's metrics.
func (d *InterruptDetector) ResetMetrics() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalCalls = 0
	d.totalLatency = 0
	d.interrupts = 0
	d.backchannels = 0
	d.errors = 0
}

// InterruptDetectorMetrics contains performance metrics.
type InterruptDetectorMetrics struct {
	TotalCalls     int64         `json:"total_calls"`
	TotalErrors    int64         `json:"total_errors"`
	Interrupts     int64         `json:"interrupts"`
	Backchannels   int64         `json:"backchannels"`
	AverageLatency time.Duration `json:"average_latency"`
}

// UpdateConfig updates the interrupt configuration.
func (d *InterruptDetector) UpdateConfig(config InterruptConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if config.Mode != "" {
		d.config.Mode = config.Mode
	}
	if config.EnergyThreshold != 0 {
		d.config.EnergyThreshold = config.EnergyThreshold
	}
	if config.DebounceMs != 0 {
		d.config.DebounceMs = config.DebounceMs
	}
	if config.SemanticCheck != nil {
		d.config.SemanticCheck = config.SemanticCheck
	}
	if config.SemanticModel != "" {
		d.config.SemanticModel = config.SemanticModel
	}
	if config.SavePartial != "" {
		d.config.SavePartial = config.SavePartial
	}
}

// --- Mock LLM for Testing ---

// MockLLMFunc creates a mock LLM function for testing.
// The response map keys are prefixes that will be matched against the prompt.
func MockLLMFunc(responses map[string]string, defaultResponse string, latency time.Duration) LLMFunc {
	return func(ctx context.Context, req LLMRequest) LLMResponse {
		// Simulate latency
		if latency > 0 {
			select {
			case <-time.After(latency):
			case <-ctx.Done():
				return LLMResponse{Error: ctx.Err()}
			}
		}

		// Check for timeout
		select {
		case <-ctx.Done():
			return LLMResponse{Error: ctx.Err()}
		default:
		}

		// Find matching response
		for prefix, resp := range responses {
			if strings.Contains(req.Prompt, prefix) {
				return LLMResponse{
					Text:      resp,
					Model:     req.Model,
					LatencyMs: latency.Milliseconds(),
				}
			}
		}

		return LLMResponse{
			Text:      defaultResponse,
			Model:     req.Model,
			LatencyMs: latency.Milliseconds(),
		}
	}
}

// ErrorLLMFunc creates an LLM function that always returns an error.
func ErrorLLMFunc(err error) LLMFunc {
	return func(ctx context.Context, req LLMRequest) LLMResponse {
		return LLMResponse{Error: err}
	}
}

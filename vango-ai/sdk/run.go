package vango

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vango-ai/vango/pkg/core/types"
	"github.com/vango-ai/vango/pkg/core/voice/tts"
)

// RunStopReason indicates why the run loop terminated.
type RunStopReason string

const (
	RunStopEndTurn      RunStopReason = "end_turn"       // Model finished naturally
	RunStopMaxToolCalls RunStopReason = "max_tool_calls" // Hit tool call limit
	RunStopMaxTurns     RunStopReason = "max_turns"      // Hit turn limit
	RunStopMaxTokens    RunStopReason = "max_tokens"     // Hit token limit
	RunStopTimeout      RunStopReason = "timeout"        // Hit timeout
	RunStopCustom       RunStopReason = "custom"         // Custom stop condition
	RunStopError        RunStopReason = "error"          // Error occurred
)

// RunResult contains the result of a tool execution loop.
type RunResult struct {
	Response      *Response     `json:"response"`
	Steps         []RunStep     `json:"steps"`
	ToolCallCount int           `json:"tool_call_count"`
	TurnCount     int           `json:"turn_count"`
	Usage         types.Usage   `json:"usage"`
	StopReason    RunStopReason `json:"stop_reason"`
}

// RunStep represents a single step in the tool execution loop.
type RunStep struct {
	Index       int                   `json:"index"`
	Response    *Response             `json:"response"`
	ToolCalls   []ToolCall            `json:"tool_calls,omitempty"`
	ToolResults []ToolExecutionResult `json:"tool_results,omitempty"`
	DurationMs  int64                 `json:"duration_ms"`
}

// ToolCall represents a tool invocation from the model.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolExecutionResult represents the result of a tool execution.
type ToolExecutionResult struct {
	ToolUseID string               `json:"tool_use_id"`
	Content   []types.ContentBlock `json:"content"`
	Error     error                `json:"-"`
	ErrorMsg  string               `json:"error,omitempty"`
}

// ToolHandler is a function that handles a tool call.
type ToolHandler func(ctx context.Context, input json.RawMessage) (any, error)

// runConfig holds configuration for the Run loop.
type runConfig struct {
	maxToolCalls  int
	maxTurns      int
	maxTokens     int
	timeout       time.Duration
	stopWhen      func(*Response) bool
	toolHandlers  map[string]ToolHandler
	beforeCall    func(*MessageRequest)
	afterResponse func(*Response)
	onToolCall    func(name string, input map[string]any, output any, err error)
	onStop        func(*RunResult)
	parallelTools bool
	toolTimeout   time.Duration
}

// defaultRunConfig returns the default run configuration.
func defaultRunConfig() runConfig {
	return runConfig{
		toolHandlers:  make(map[string]ToolHandler),
		parallelTools: true,
		toolTimeout:   30 * time.Second,
	}
}

// RunOption configures the Run loop.
type RunOption func(*runConfig)

// WithMaxToolCalls sets the maximum number of tool calls before stopping.
func WithMaxToolCalls(n int) RunOption {
	return func(c *runConfig) { c.maxToolCalls = n }
}

// WithMaxTurns sets the maximum number of LLM turns before stopping.
func WithMaxTurns(n int) RunOption {
	return func(c *runConfig) { c.maxTurns = n }
}

// WithMaxTokensRun sets the maximum total tokens before stopping.
func WithMaxTokensRun(n int) RunOption {
	return func(c *runConfig) { c.maxTokens = n }
}

// WithRunTimeout sets a timeout for the entire Run loop.
func WithRunTimeout(d time.Duration) RunOption {
	return func(c *runConfig) { c.timeout = d }
}

// WithStopWhen sets a custom stop condition.
// The function is called after each response.
// If it returns true, the run stops.
func WithStopWhen(fn func(*Response) bool) RunOption {
	return func(c *runConfig) { c.stopWhen = fn }
}

// WithToolHandler registers a handler for a specific tool.
func WithToolHandler(name string, fn ToolHandler) RunOption {
	return func(c *runConfig) {
		if c.toolHandlers == nil {
			c.toolHandlers = make(map[string]ToolHandler)
		}
		c.toolHandlers[name] = fn
	}
}

// WithToolHandlers registers multiple tool handlers.
func WithToolHandlers(handlers map[string]ToolHandler) RunOption {
	return func(c *runConfig) {
		if c.toolHandlers == nil {
			c.toolHandlers = make(map[string]ToolHandler)
		}
		for name, fn := range handlers {
			c.toolHandlers[name] = fn
		}
	}
}

// WithTools registers tools that have embedded handlers (from MakeTool).
func WithTools(tools ...ToolWithHandler) RunOption {
	return func(c *runConfig) {
		if c.toolHandlers == nil {
			c.toolHandlers = make(map[string]ToolHandler)
		}
		for _, t := range tools {
			if t.Handler != nil && t.Name != "" {
				c.toolHandlers[t.Name] = t.Handler
			}
		}
	}
}

// WithToolSet registers all handlers from a ToolSet.
func WithToolSet(ts *ToolSet) RunOption {
	return func(c *runConfig) {
		if c.toolHandlers == nil {
			c.toolHandlers = make(map[string]ToolHandler)
		}
		for name, handler := range ts.Handlers() {
			c.toolHandlers[name] = handler
		}
	}
}

// WithBeforeCall sets a hook called before each LLM call.
func WithBeforeCall(fn func(*MessageRequest)) RunOption {
	return func(c *runConfig) { c.beforeCall = fn }
}

// WithAfterResponse sets a hook called after each LLM response.
func WithAfterResponse(fn func(*Response)) RunOption {
	return func(c *runConfig) { c.afterResponse = fn }
}

// WithOnToolCall sets a hook called after each tool execution.
func WithOnToolCall(fn func(name string, input map[string]any, output any, err error)) RunOption {
	return func(c *runConfig) { c.onToolCall = fn }
}

// WithOnStop sets a hook called when the loop stops.
func WithOnStop(fn func(*RunResult)) RunOption {
	return func(c *runConfig) { c.onStop = fn }
}

// WithParallelTools enables parallel execution of independent tool calls.
// Default is true.
func WithParallelTools(enabled bool) RunOption {
	return func(c *runConfig) { c.parallelTools = enabled }
}

// WithToolTimeout sets a timeout for individual tool executions.
// Default is 30 seconds.
func WithToolTimeout(d time.Duration) RunOption {
	return func(c *runConfig) { c.toolTimeout = d }
}

// --- Run Loop Implementation ---

// runLoop executes the main tool execution loop.
func (s *MessagesService) runLoop(ctx context.Context, req *MessageRequest, cfg *runConfig) (*RunResult, error) {
	result := &RunResult{
		Steps: make([]RunStep, 0),
	}

	// Create a working copy of messages
	messages := make([]types.Message, len(req.Messages))
	copy(messages, req.Messages)

	// Apply timeout if configured
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	// Main loop
	for {
		// Check timeout
		select {
		case <-ctx.Done():
			result.StopReason = RunStopTimeout
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, ctx.Err()
		default:
		}

		// Check turn limit
		if cfg.maxTurns > 0 && result.TurnCount >= cfg.maxTurns {
			result.StopReason = RunStopMaxTurns
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Check token limit
		if cfg.maxTokens > 0 && result.Usage.TotalTokens >= cfg.maxTokens {
			result.StopReason = RunStopMaxTokens
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Build request for this turn
		turnReq := &types.MessageRequest{
			Model:         req.Model,
			Messages:      messages,
			MaxTokens:     req.MaxTokens,
			System:        req.System,
			Temperature:   req.Temperature,
			TopP:          req.TopP,
			TopK:          req.TopK,
			StopSequences: req.StopSequences,
			Tools:         req.Tools,
			ToolChoice:    req.ToolChoice,
			OutputFormat:  req.OutputFormat,
			Output:        req.Output,
			Voice:         req.Voice,
			Extensions:    req.Extensions,
			Metadata:      req.Metadata,
		}

		// Call before hook
		if cfg.beforeCall != nil {
			cfg.beforeCall(turnReq)
		}

		// Make the API call
		stepStart := time.Now()
		resp, err := s.Create(ctx, turnReq)
		stepDuration := time.Since(stepStart).Milliseconds()

		if err != nil {
			result.StopReason = RunStopError
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, err
		}

		// Call after hook
		if cfg.afterResponse != nil {
			cfg.afterResponse(resp)
		}

		// Aggregate usage
		result.Usage = result.Usage.Add(resp.Usage)
		result.TurnCount++

		// Create step record
		step := RunStep{
			Index:      len(result.Steps),
			Response:   resp,
			DurationMs: stepDuration,
		}

		// Check custom stop condition
		if cfg.stopWhen != nil && cfg.stopWhen(resp) {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopCustom
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Check if model finished without tool calls
		if resp.StopReason != types.StopReasonToolUse {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopEndTurn
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Process tool calls
		toolUses := resp.ToolUses()
		if len(toolUses) == 0 {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopEndTurn
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Check tool call limit
		if cfg.maxToolCalls > 0 && result.ToolCallCount+len(toolUses) > cfg.maxToolCalls {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopMaxToolCalls
			if cfg.onStop != nil {
				cfg.onStop(result)
			}
			return result, nil
		}

		// Execute tool calls
		toolResults := s.executeToolCalls(ctx, toolUses, cfg)

		step.ToolCalls = make([]ToolCall, len(toolUses))
		for i, tu := range toolUses {
			step.ToolCalls[i] = ToolCall{
				ID:    tu.ID,
				Name:  tu.Name,
				Input: tu.Input,
			}
		}
		step.ToolResults = toolResults
		result.Steps = append(result.Steps, step)
		result.ToolCallCount += len(toolUses)

		// Append assistant message with tool calls
		messages = append(messages, types.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		// Append tool results as user message
		toolResultBlocks := make([]types.ContentBlock, len(toolResults))
		for i, tr := range toolResults {
			toolResultBlocks[i] = types.ToolResultBlock{
				Type:      "tool_result",
				ToolUseID: tr.ToolUseID,
				Content:   tr.Content,
				IsError:   tr.Error != nil,
			}
		}
		messages = append(messages, types.Message{
			Role:    "user",
			Content: toolResultBlocks,
		})
	}
}

// executeToolCalls executes all tool calls, either in parallel or sequentially.
func (s *MessagesService) executeToolCalls(ctx context.Context, toolUses []types.ToolUseBlock, cfg *runConfig) []ToolExecutionResult {
	results := make([]ToolExecutionResult, len(toolUses))

	if cfg.parallelTools && len(toolUses) > 1 {
		// Execute in parallel
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i, tu := range toolUses {
			wg.Add(1)
			go func(idx int, toolUse types.ToolUseBlock) {
				defer wg.Done()
				result := s.executeToolCall(ctx, toolUse, cfg)
				mu.Lock()
				results[idx] = result
				mu.Unlock()
			}(i, tu)
		}
		wg.Wait()
	} else {
		// Execute sequentially
		for i, tu := range toolUses {
			results[i] = s.executeToolCall(ctx, tu, cfg)
		}
	}

	return results
}

// executeToolCall executes a single tool call.
func (s *MessagesService) executeToolCall(ctx context.Context, toolUse types.ToolUseBlock, cfg *runConfig) ToolExecutionResult {
	result := ToolExecutionResult{
		ToolUseID: toolUse.ID,
	}

	// Apply tool timeout
	if cfg.toolTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.toolTimeout)
		defer cancel()
	}

	// Find handler
	handler, ok := cfg.toolHandlers[toolUse.Name]
	if !ok {
		// No handler - return a generic message
		result.Content = []types.ContentBlock{
			types.TextBlock{
				Type: "text",
				Text: fmt.Sprintf("Tool '%s' was called but no handler is registered.", toolUse.Name),
			},
		}
		return result
	}

	// Marshal input
	inputJSON, err := json.Marshal(toolUse.Input)
	if err != nil {
		result.Error = err
		result.ErrorMsg = err.Error()
		result.Content = []types.ContentBlock{
			types.TextBlock{
				Type: "text",
				Text: fmt.Sprintf("Error marshaling tool input: %v", err),
			},
		}
		return result
	}

	// Execute handler
	output, err := handler(ctx, inputJSON)

	// Call hook
	if cfg.onToolCall != nil {
		cfg.onToolCall(toolUse.Name, toolUse.Input, output, err)
	}

	if err != nil {
		result.Error = err
		result.ErrorMsg = err.Error()
		result.Content = []types.ContentBlock{
			types.TextBlock{
				Type: "text",
				Text: fmt.Sprintf("Error executing tool: %v", err),
			},
		}
		return result
	}

	// Convert output to content
	result.Content = outputToContentBlocks(output)

	return result
}

// outputToContentBlocks converts tool output to content blocks.
func outputToContentBlocks(output any) []types.ContentBlock {
	switch v := output.(type) {
	case string:
		return []types.ContentBlock{
			types.TextBlock{Type: "text", Text: v},
		}
	case []types.ContentBlock:
		return v
	case types.ContentBlock:
		return []types.ContentBlock{v}
	default:
		// JSON encode other types
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return []types.ContentBlock{
				types.TextBlock{Type: "text", Text: fmt.Sprintf("%v", v)},
			}
		}
		return []types.ContentBlock{
			types.TextBlock{Type: "text", Text: string(jsonBytes)},
		}
	}
}

// --- RunStream Implementation ---

// RunStream wraps a streaming tool execution loop.
type RunStream struct {
	events    chan RunStreamEvent
	result    *RunResult
	err       error
	closed    atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

// RunStreamEvent is an event from the RunStream.
type RunStreamEvent interface {
	runStreamEventType() string
}

// StepStartEvent signals the start of a new step.
type StepStartEvent struct {
	Index int `json:"index"`
}

func (e StepStartEvent) runStreamEventType() string { return "step_start" }

// StreamEventWrapper wraps regular stream events from the underlying stream.
type StreamEventWrapper struct {
	Event types.StreamEvent `json:"event"`
}

func (e StreamEventWrapper) runStreamEventType() string { return "stream_event" }

// ToolCallStartEvent signals the start of a tool call.
type ToolCallStartEvent struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (e ToolCallStartEvent) runStreamEventType() string { return "tool_call_start" }

// ToolResultEvent contains the result of a tool call.
type ToolResultEvent struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Content []types.ContentBlock `json:"content"`
	Error   error                `json:"-"`
}

func (e ToolResultEvent) runStreamEventType() string { return "tool_result" }

// StepCompleteEvent signals the completion of a step.
type StepCompleteEvent struct {
	Index    int       `json:"index"`
	Response *Response `json:"response"`
}

func (e StepCompleteEvent) runStreamEventType() string { return "step_complete" }

// RunCompleteEvent signals the run is complete.
type RunCompleteEvent struct {
	Result *RunResult `json:"result"`
}

func (e RunCompleteEvent) runStreamEventType() string { return "run_complete" }

// AudioChunkEvent contains streaming audio data from TTS.
type AudioChunkEvent struct {
	Data   []byte `json:"data"`
	Format string `json:"format"`
}

func (e AudioChunkEvent) runStreamEventType() string { return "audio_chunk" }

// voiceStreamer manages text batching and TTS streaming.
type voiceStreamer struct {
	ttsCtx     *tts.StreamingContext
	sendEvents func(RunStreamEvent)
	format     string

	// Text batching
	buffer     strings.Builder
	bufferMu   sync.Mutex
	flushTimer *time.Timer
	done       chan struct{}
	wg         sync.WaitGroup

	// Config
	maxChars     int           // Send after this many chars
	maxDelay     time.Duration // Max time before sending buffered text
	sentenceEnds string        // Characters that trigger immediate send
}

func newVoiceStreamer(ttsCtx *tts.StreamingContext, format string, sendEvents func(RunStreamEvent)) *voiceStreamer {
	vs := &voiceStreamer{
		ttsCtx:       ttsCtx,
		sendEvents:   sendEvents,
		format:       format,
		done:         make(chan struct{}),
		maxChars:     80,             // Send every ~80 chars
		maxDelay:     150 * time.Millisecond, // Or after 150ms
		sentenceEnds: ".!?",
	}

	// Start audio forwarding goroutine
	vs.wg.Add(1)
	go vs.forwardAudio()

	return vs
}

// AddText adds text to the buffer and may trigger a send.
func (vs *voiceStreamer) AddText(text string) {
	vs.bufferMu.Lock()
	defer vs.bufferMu.Unlock()

	vs.buffer.WriteString(text)
	content := vs.buffer.String()

	// Check if we should send now
	shouldSend := false

	// Send if buffer is large enough
	if len(content) >= vs.maxChars {
		shouldSend = true
	}

	// Send on sentence boundaries
	if len(text) > 0 && strings.ContainsAny(text, vs.sentenceEnds) {
		shouldSend = true
	}

	if shouldSend {
		vs.sendBufferLocked()
	} else {
		// Reset/start the flush timer
		vs.resetTimerLocked()
	}
}

func (vs *voiceStreamer) sendBufferLocked() {
	content := strings.TrimSpace(vs.buffer.String())
	if content == "" {
		return
	}

	vs.buffer.Reset()
	if vs.flushTimer != nil {
		vs.flushTimer.Stop()
		vs.flushTimer = nil
	}

	// Send to TTS (continue=true, more text coming)
	vs.ttsCtx.SendText(content, false)
}

func (vs *voiceStreamer) resetTimerLocked() {
	if vs.flushTimer != nil {
		vs.flushTimer.Stop()
	}
	vs.flushTimer = time.AfterFunc(vs.maxDelay, func() {
		vs.bufferMu.Lock()
		defer vs.bufferMu.Unlock()
		vs.sendBufferLocked()
	})
}

// Flush sends any remaining text and signals completion.
func (vs *voiceStreamer) Flush() {
	vs.bufferMu.Lock()
	content := strings.TrimSpace(vs.buffer.String())
	vs.buffer.Reset()
	if vs.flushTimer != nil {
		vs.flushTimer.Stop()
		vs.flushTimer = nil
	}
	vs.bufferMu.Unlock()

	if content != "" {
		// Send final text chunk
		vs.ttsCtx.SendText(content, true)
	} else {
		// Just flush
		vs.ttsCtx.Flush()
	}
}

// Close waits for all audio to be forwarded, then cleans up.
func (vs *voiceStreamer) Close() {
	// Wait for audio channel to be closed (all audio received)
	// Don't close vs.done yet - let forwardAudio drain naturally
	vs.wg.Wait()

	// Now safe to close
	close(vs.done)
	vs.ttsCtx.Close()
}

// forwardAudio forwards audio chunks as events.
func (vs *voiceStreamer) forwardAudio() {
	defer vs.wg.Done()

	// Simply drain the audio channel until it's closed
	// The channel is closed when TTS context receives "done" from Cartesia
	for chunk := range vs.ttsCtx.Audio() {
		vs.sendEvents(AudioChunkEvent{
			Data:   chunk,
			Format: vs.format,
		})
	}
}

// runStreamLoop executes the streaming tool loop.
func (s *MessagesService) runStreamLoop(ctx context.Context, req *MessageRequest, cfg *runConfig) *RunStream {
	rs := &RunStream{
		events: make(chan RunStreamEvent, 100),
		done:   make(chan struct{}),
	}

	go rs.run(ctx, s, req, cfg)
	return rs
}

func (rs *RunStream) run(ctx context.Context, svc *MessagesService, req *MessageRequest, cfg *runConfig) {
	defer rs.closeOnce.Do(func() {
		close(rs.events)
		close(rs.done)
	})

	result := &RunResult{
		Steps: make([]RunStep, 0),
	}

	// Create working copy of messages
	messages := make([]types.Message, len(req.Messages))
	copy(messages, req.Messages)

	// Apply timeout
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	// Set up voice streaming if configured
	var voiceStream *voiceStreamer
	if req.Voice != nil && req.Voice.Output != nil && svc.client.voicePipeline != nil {
		format := req.Voice.Output.Format
		if format == "" {
			format = "wav"
		}

		ttsCtx, err := svc.client.voicePipeline.NewStreamingTTSContext(ctx, req.Voice)
		if err == nil {
			voiceStream = newVoiceStreamer(ttsCtx, format, rs.send)
		}
		// If TTS setup fails, continue without voice
	}

	// Helper to flush voice and wait for all audio before completing
	voiceFinished := false
	finishVoice := func() {
		if voiceStream != nil && !voiceFinished {
			voiceFinished = true
			voiceStream.Flush()
			voiceStream.Close()
		}
	}
	defer finishVoice() // Ensure cleanup on any exit

	stepIndex := 0

	for {
		select {
		case <-ctx.Done():
			result.StopReason = RunStopTimeout
			rs.result = result
			rs.err = ctx.Err()
			return
		default:
		}

		// Check limits
		if cfg.maxTurns > 0 && result.TurnCount >= cfg.maxTurns {
			result.StopReason = RunStopMaxTurns
			rs.result = result
			finishVoice()
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		if cfg.maxTokens > 0 && result.Usage.TotalTokens >= cfg.maxTokens {
			result.StopReason = RunStopMaxTokens
			rs.result = result
			finishVoice()
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		// Signal step start
		rs.send(StepStartEvent{Index: stepIndex})

		// Build request
		turnReq := &types.MessageRequest{
			Model:         req.Model,
			Messages:      messages,
			MaxTokens:     req.MaxTokens,
			System:        req.System,
			Temperature:   req.Temperature,
			TopP:          req.TopP,
			TopK:          req.TopK,
			StopSequences: req.StopSequences,
			Tools:         req.Tools,
			ToolChoice:    req.ToolChoice,
			Stream:        true, // Always stream in RunStream
			OutputFormat:  req.OutputFormat,
			Output:        req.Output,
			Voice:         req.Voice,
			Extensions:    req.Extensions,
			Metadata:      req.Metadata,
		}

		if cfg.beforeCall != nil {
			cfg.beforeCall(turnReq)
		}

		stepStart := time.Now()

		// Stream this turn
		stream, err := svc.Stream(ctx, turnReq)
		if err != nil {
			result.StopReason = RunStopError
			rs.result = result
			rs.err = err
			return
		}

		// Track tool blocks as they're being built
		type pendingTool struct {
			id        string
			name      string
			inputJSON strings.Builder // Accumulate input JSON from deltas
			emitted   bool            // Whether we've emitted the start event
		}
		pendingTools := make(map[int]*pendingTool) // index -> tool info

		// Forward stream events and extract text for voice
		for event := range stream.Events() {
			rs.send(StreamEventWrapper{Event: event})

			// Detect tool calls from content_block_start events
			if startEvent, ok := event.(types.ContentBlockStartEvent); ok {
				switch block := startEvent.ContentBlock.(type) {
				case types.ToolUseBlock:
					// Client-side tool use (will be executed by SDK)
					pendingTools[startEvent.Index] = &pendingTool{
						id:   block.ID,
						name: block.Name,
					}
					// Emit start event immediately (input may be empty, will be filled via deltas)
					rs.send(ToolCallStartEvent{ID: block.ID, Name: block.Name, Input: block.Input})
					pendingTools[startEvent.Index].emitted = true

				case types.ServerToolUseBlock:
					// Server-side tool use (executed by Anthropic - e.g., web_search)
					pendingTools[startEvent.Index] = &pendingTool{
						id:   block.ID,
						name: block.Name,
					}
					// Emit start event immediately for real-time feedback
					rs.send(ToolCallStartEvent{ID: block.ID, Name: block.Name, Input: block.Input})
					pendingTools[startEvent.Index].emitted = true

				case types.WebSearchToolResultBlock:
					// Server-side tool result - convert to content blocks for display
					var resultContent []types.ContentBlock
					if len(block.Content) > 0 {
						// Create a summary of search results
						var summary strings.Builder
						summary.WriteString(fmt.Sprintf("Found %d results", len(block.Content)))
						resultContent = []types.ContentBlock{
							types.TextBlock{Type: "text", Text: summary.String()},
						}
					}
					rs.send(ToolResultEvent{
						ID:      block.ToolUseID,
						Name:    "web_search",
						Content: resultContent,
					})
				}
			}

			// Accumulate tool input from input_json_delta events
			if deltaEvent, ok := event.(types.ContentBlockDeltaEvent); ok {
				if inputDelta, ok := deltaEvent.Delta.(types.InputJSONDelta); ok {
					if pt, exists := pendingTools[deltaEvent.Index]; exists {
						pt.inputJSON.WriteString(inputDelta.PartialJSON)
					}
				}
			}

			// Emit tool call start when block completes (with full input)
			if stopEvent, ok := event.(types.ContentBlockStopEvent); ok {
				if pt, exists := pendingTools[stopEvent.Index]; exists {
					if !pt.emitted {
						// Parse accumulated input JSON
						var input map[string]any
						if pt.inputJSON.Len() > 0 {
							json.Unmarshal([]byte(pt.inputJSON.String()), &input)
						}
						rs.send(ToolCallStartEvent{ID: pt.id, Name: pt.name, Input: input})
					}
					delete(pendingTools, stopEvent.Index)
				}
			}

			// Feed text deltas to voice streamer
			if voiceStream != nil {
				if deltaEvent, ok := event.(types.ContentBlockDeltaEvent); ok {
					if textDelta, ok := deltaEvent.Delta.(types.TextDelta); ok {
						voiceStream.AddText(textDelta.Text)
					}
				}
			}
		}

		// EOF is normal stream termination, not an error
		if streamErr := stream.Err(); streamErr != nil && streamErr != io.EOF {
			result.StopReason = RunStopError
			rs.result = result
			rs.err = streamErr
			stream.Close()
			return
		}

		coreResp := stream.Response()
		stream.Close()

		resp := &Response{MessageResponse: coreResp}
		stepDuration := time.Since(stepStart).Milliseconds()

		if cfg.afterResponse != nil {
			cfg.afterResponse(resp)
		}

		result.Usage = result.Usage.Add(resp.Usage)
		result.TurnCount++

		step := RunStep{
			Index:      stepIndex,
			Response:   resp,
			DurationMs: stepDuration,
		}

		// Check custom stop
		if cfg.stopWhen != nil && cfg.stopWhen(resp) {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopCustom
			rs.send(StepCompleteEvent{Index: stepIndex, Response: resp})
			finishVoice()
			rs.result = result
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		// Check if done
		if resp.StopReason != types.StopReasonToolUse {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopEndTurn
			rs.send(StepCompleteEvent{Index: stepIndex, Response: resp})
			finishVoice()
			rs.result = result
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		// Process tool calls
		toolUses := resp.ToolUses()
		if len(toolUses) == 0 {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopEndTurn
			rs.send(StepCompleteEvent{Index: stepIndex, Response: resp})
			finishVoice()
			rs.result = result
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		// Check tool call limit
		if cfg.maxToolCalls > 0 && result.ToolCallCount+len(toolUses) > cfg.maxToolCalls {
			result.Response = resp
			result.Steps = append(result.Steps, step)
			result.StopReason = RunStopMaxToolCalls
			rs.send(StepCompleteEvent{Index: stepIndex, Response: resp})
			finishVoice()
			rs.result = result
			rs.send(RunCompleteEvent{Result: result})
			return
		}

		// Execute tools with events
		toolResults := make([]ToolExecutionResult, len(toolUses))
		for i, tu := range toolUses {
			rs.send(ToolCallStartEvent{ID: tu.ID, Name: tu.Name, Input: tu.Input})

			tr := svc.executeToolCall(ctx, tu, cfg)
			toolResults[i] = tr

			rs.send(ToolResultEvent{ID: tu.ID, Name: tu.Name, Content: tr.Content, Error: tr.Error})
		}

		step.ToolCalls = make([]ToolCall, len(toolUses))
		for i, tu := range toolUses {
			step.ToolCalls[i] = ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input}
		}
		step.ToolResults = toolResults
		result.Steps = append(result.Steps, step)
		result.ToolCallCount += len(toolUses)

		rs.send(StepCompleteEvent{Index: stepIndex, Response: resp})

		// Append messages for next turn
		messages = append(messages, types.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		toolResultBlocks := make([]types.ContentBlock, len(toolResults))
		for i, tr := range toolResults {
			toolResultBlocks[i] = types.ToolResultBlock{
				Type:      "tool_result",
				ToolUseID: tr.ToolUseID,
				Content:   tr.Content,
				IsError:   tr.Error != nil,
			}
		}
		messages = append(messages, types.Message{
			Role:    "user",
			Content: toolResultBlocks,
		})

		stepIndex++
	}
}

func (rs *RunStream) send(event RunStreamEvent) {
	if rs.closed.Load() {
		return
	}
	select {
	case rs.events <- event:
	case <-rs.done:
	}
}

// Events returns the channel of run stream events.
func (rs *RunStream) Events() <-chan RunStreamEvent {
	return rs.events
}

// Result returns the final result after the stream ends.
func (rs *RunStream) Result() *RunResult {
	<-rs.done
	return rs.result
}

// Err returns any error that occurred.
func (rs *RunStream) Err() error {
	<-rs.done
	return rs.err
}

// Close stops the run stream.
func (rs *RunStream) Close() error {
	if rs.closed.Swap(true) {
		return nil
	}
	// Only close done channel once; the closeOnce ensures this
	// Note: done might already be closed by run() goroutine via closeOnce
	rs.closeOnce.Do(func() {
		close(rs.done)
	})
	return nil
}

package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shillcollin/gai/core"
)

// Runner orchestrates multi-step tool execution using a provider.
type Runner struct {
	provider    core.Provider
	maxParallel int
	toolTimeout time.Duration
	errorMode   ToolErrorMode
	retry       ToolRetry
}

// RunnerOption configures the runner.
type RunnerOption func(*Runner)

// WithMaxParallel sets the maximum number of tool executions per step.
func WithMaxParallel(n int) RunnerOption {
	return func(r *Runner) {
		if n > 0 {
			r.maxParallel = n
		}
	}
}

// WithToolTimeout sets the per-tool timeout.
func WithToolTimeout(d time.Duration) RunnerOption {
	return func(r *Runner) {
		if d > 0 {
			r.toolTimeout = d
		}
	}
}

// ToolErrorMode controls error handling.
type ToolErrorMode int

const (
	ToolErrorPropagate ToolErrorMode = iota
	ToolErrorAppendAndContinue
)

// WithOnToolError configures error propagation.
func WithOnToolError(mode ToolErrorMode) RunnerOption {
	return func(r *Runner) { r.errorMode = mode }
}

// ToolRetry controls tool retry behaviour.
type ToolRetry struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// WithToolRetry sets retry options.
func WithToolRetry(retry ToolRetry) RunnerOption {
	return func(r *Runner) { r.retry = retry }
}

// New creates a new runner.
func New(provider core.Provider, opts ...RunnerOption) *Runner {
	r := &Runner{
		provider:    provider,
		maxParallel: 1,
		toolTimeout: 30 * time.Second,
		errorMode:   ToolErrorPropagate,
		retry:       ToolRetry{MaxAttempts: 1, BaseDelay: 250 * time.Millisecond},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ExecuteRequest runs the request to completion, handling tool calls until stop conditions are met.
func (r *Runner) ExecuteRequest(ctx context.Context, req core.Request) (*core.TextResult, error) {
	state := &core.RunnerState{
		Messages: append([]core.Message(nil), req.Messages...),
		Steps:    []core.Step{},
	}

	for {
		if req.StopWhen != nil {
			stop, reason := req.StopWhen(state)
			if stop {
				state.StopReason = reason
				break
			}
		}
		step, err := r.executeStep(ctx, req, state)
		if err != nil {
			return nil, err
		}
		state.Steps = append(state.Steps, step)
		state.LastStep = &step
		if len(step.ToolCalls) == 0 {
			state.StopReason = core.StopReason{Type: "no_more_tools"}
			break
		}
	}

	return &core.TextResult{
		Text:         state.LastText(),
		Steps:        state.Steps,
		Usage:        state.Usage,
		FinishReason: state.StopReason,
		Provider:     r.provider.Capabilities().Provider,
	}, nil
}

func (r *Runner) executeStep(ctx context.Context, req core.Request, state *core.RunnerState) (core.Step, error) {
	stepNum := len(state.Steps) + 1
	stream, err := r.provider.StreamText(ctx, core.Request{
		Model:           req.Model,
		Messages:        append([]core.Message(nil), state.Messages...),
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		TopP:            req.TopP,
		TopK:            req.TopK,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		ProviderOptions: req.ProviderOptions,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return core.Step{}, err
	}
	defer stream.Close()

	builder := &strings.Builder{}
	var toolCalls []core.ToolExecution
	usage := core.Usage{}

	for event := range stream.Events() {
		switch event.Type {
		case core.EventTextDelta:
			builder.WriteString(event.TextDelta)
		case core.EventToolCall:
			exec := core.ToolExecution{Call: event.ToolCall}
			toolCalls = append(toolCalls, exec)
		case core.EventFinish:
			if event.FinishReason != nil {
				state.StopReason = *event.FinishReason
			}
			usage = event.Usage
		case core.EventError:
			if event.Error != nil {
				return core.Step{}, event.Error
			}
		}
	}
	if err := stream.Err(); err != nil {
		return core.Step{}, err
	}

	step := core.Step{
		Number:     stepNum,
		Text:       builder.String(),
		ToolCalls:  []core.ToolExecution{},
		Usage:      usage,
		DurationMS: 0,
	}

	state.Messages = append(state.Messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.Text{Text: step.Text}}})

	if len(toolCalls) == 0 {
		state.Usage.InputTokens += usage.InputTokens
		state.Usage.OutputTokens += usage.OutputTokens
		state.Usage.TotalTokens += usage.TotalTokens
		return step, nil
	}

	executions, err := r.invokeTools(ctx, toolCalls, req.Tools, stepNum)
	if err != nil && r.errorMode == ToolErrorPropagate {
		return core.Step{}, err
	}
	step.ToolCalls = executions
	state.Messages = append(state.Messages, buildToolResultMessages(executions)...)
	state.Usage.InputTokens += usage.InputTokens
	state.Usage.OutputTokens += usage.OutputTokens
	state.Usage.TotalTokens += usage.TotalTokens
	return step, nil
}

func (r *Runner) invokeTools(ctx context.Context, calls []core.ToolExecution, tools []core.ToolHandle, stepID int) ([]core.ToolExecution, error) {
	toolIndex := map[string]core.ToolHandle{}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		toolIndex[tool.Name()] = tool
	}
	results := make([]core.ToolExecution, len(calls))
	var wg sync.WaitGroup
	errCh := make(chan error, len(calls))
	sem := make(chan struct{}, r.maxParallel)

	for i, call := range calls {
		handle, ok := toolIndex[call.Call.Name]
		if !ok {
			errCh <- fmt.Errorf("tool %s not registered", call.Call.Name)
			continue
		}
		results[i] = call
		wg.Add(1)
		go func(idx int, handle core.ToolHandle, call core.ToolExecution) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			execResult, err := r.executeTool(ctx, handle, call.Call, stepID)
			results[idx] = execResult
			if err != nil {
				errCh <- err
			}
		}(i, handle, call)
	}

	wg.Wait()
	close(errCh)

	var err error
	for e := range errCh {
		if e != nil {
			err = e
			if r.errorMode == ToolErrorPropagate {
				break
			}
		}
	}
	return results, err
}

func (r *Runner) executeTool(ctx context.Context, handle core.ToolHandle, call core.ToolCall, stepID int) (core.ToolExecution, error) {
	attempts := 0
	var lastErr error
	for attempts < max(1, r.retry.MaxAttempts) {
		attempts++
		ctxExec, cancel := context.WithTimeout(ctx, r.toolTimeout)
		meta := core.ToolMeta{CallID: call.ID, StepID: stepID}
		result, err := handle.Execute(ctxExec, call.Input, meta)
		cancel()
		if err == nil {
			return core.ToolExecution{Call: call, Result: result, Retries: attempts - 1}, nil
		}
		lastErr = err
		if attempts >= r.retry.MaxAttempts {
			break
		}
		time.Sleep(r.retry.BaseDelay * time.Duration(attempts))
	}
	exec := core.ToolExecution{Call: call, Error: lastErr, Retries: attempts - 1}
	return exec, lastErr
}

func buildToolResultMessages(executions []core.ToolExecution) []core.Message {
	var messages []core.Message
	for _, exec := range executions {
		if exec.Call.Name == "" {
			continue
		}
		if exec.Error != nil {
			messages = append(messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.Text{Text: fmt.Sprintf("tool %s failed: %v", exec.Call.Name, exec.Error)}}})
			continue
		}
		messages = append(messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.ToolCall{ID: exec.Call.ID, Name: exec.Call.Name, Input: exec.Call.Input}}})
		messages = append(messages, core.Message{Role: core.User, Parts: []core.Part{core.ToolResult{ID: exec.Call.ID, Name: exec.Call.Name, Result: exec.Result}}})
	}
	return messages
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

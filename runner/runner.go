package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/obs"
	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/skills"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	jitterMu   sync.Mutex
	jitterRand = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// Runner orchestrates multi-step tool execution using a provider.
type Runner struct {
	provider     core.Provider
	maxParallel  int
	toolTimeout  time.Duration
	errorMode    ToolErrorMode
	retry        ToolRetry
	memo         ToolMemo
	interceptors []Interceptor
	skillRuntime *skillRuntime
}

// ToolMemo provides caching for tool results.
type ToolMemo interface {
	Get(name string, inputHash string) (any, bool)
	Set(name string, inputHash string, result any)
}

// Interceptor hooks into runner lifecycle events.
type Interceptor interface {
	BeforeStep(ctx context.Context, req core.Request, state *core.RunnerState, stepNumber int) context.Context
	AfterStep(ctx context.Context, step core.Step, state *core.RunnerState)
	BeforeTool(ctx context.Context, call core.ToolCall, meta *core.ToolMeta) context.Context
	AfterTool(ctx context.Context, exec *core.ToolExecution, meta *core.ToolMeta)
}

type noopInterceptor struct{}

func (noopInterceptor) BeforeStep(ctx context.Context, _ core.Request, _ *core.RunnerState, _ int) context.Context {
	return ctx
}
func (noopInterceptor) AfterStep(context.Context, core.Step, *core.RunnerState) {}
func (noopInterceptor) BeforeTool(ctx context.Context, _ core.ToolCall, _ *core.ToolMeta) context.Context {
	return ctx
}
func (noopInterceptor) AfterTool(context.Context, *core.ToolExecution, *core.ToolMeta) {}

type streamPublisher struct {
	stream   *core.Stream
	provider string
	seq      int
}

type skillRuntime struct {
	skill   *skills.Skill
	manager *sandbox.Manager
	assets  sandbox.SessionAssets
}

func (p *streamPublisher) push(event core.StreamEvent, step int) {
	if p == nil || p.stream == nil {
		return
	}
	clone := event
	clone.StepID = step
	if clone.Schema == "" {
		clone.Schema = "gai.events.v1"
	}
	if clone.Provider == "" {
		clone.Provider = p.provider
	}
	if clone.Timestamp.IsZero() {
		clone.Timestamp = time.Now()
	}
	if clone.StreamID == "" {
		if step == 0 {
			clone.StreamID = "runner"
		} else {
			clone.StreamID = fmt.Sprintf("runner.step.%d", step)
		}
	}
	p.seq++
	clone.Seq = p.seq
	p.stream.Push(clone)
}

func (p *streamPublisher) emitRunnerStart() {
	p.push(core.StreamEvent{Type: core.EventStart}, 0)
}

func (p *streamPublisher) emitRunnerFinish(state *core.RunnerState) {
	if p == nil || state == nil {
		return
	}
	reason := state.StopReason
	p.push(core.StreamEvent{
		Type:         core.EventFinish,
		FinishReason: &reason,
		Usage:        state.Usage,
	}, 0)
}

func (p *streamPublisher) emitStepStart(step int) {
	p.push(core.StreamEvent{Type: core.EventStepStart}, step)
}

func (p *streamPublisher) emitStepFinish(step core.Step) {
	p.push(core.StreamEvent{
		Type:       core.EventStepFinish,
		Usage:      step.Usage,
		DurationMS: step.DurationMS,
		ToolCalls:  len(step.ToolCalls),
	}, step.Number)
}

func (p *streamPublisher) emitToolResult(step int, exec core.ToolExecution) {
	result := core.ToolResult{ID: exec.Call.ID, Name: exec.Call.Name, Result: exec.Result}
	if exec.Error != nil {
		result.Error = exec.Error.Error()
	}
	event := core.StreamEvent{Type: core.EventToolResult, ToolResult: result}
	ext := map[string]any{}
	if exec.DurationMS > 0 {
		ext["duration_ms"] = exec.DurationMS
	}
	if exec.Retries > 0 {
		ext["retries"] = exec.Retries
	}
	if len(exec.Call.Metadata) > 0 {
		ext["metadata"] = cloneGenericMap(exec.Call.Metadata)
	}
	if len(ext) > 0 {
		event.Ext = ext
	}
	p.push(event, step)
}

func cloneGenericMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (p *streamPublisher) addWarnings(warnings ...core.Warning) {
	if p == nil || p.stream == nil || len(warnings) == 0 {
		return
	}
	p.stream.AddWarnings(warnings...)
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
	Jitter      bool
	MaxDelay    time.Duration
}

// WithToolRetry sets retry options.
func WithToolRetry(retry ToolRetry) RunnerOption {
	return func(r *Runner) { r.retry = retry }
}

// WithMemo wires a memoization cache for tool executions.
func WithMemo(m ToolMemo) RunnerOption {
	return func(r *Runner) { r.memo = m }
}

// WithInterceptor registers an interceptor for lifecycle hooks.
func WithInterceptor(i Interceptor) RunnerOption {
	return func(r *Runner) {
		if i != nil {
			r.interceptors = append(r.interceptors, i)
		}
	}
}

// WithSkill binds a skill and sandbox manager to the runner.
func WithSkill(skill *skills.Skill, manager *sandbox.Manager) RunnerOption {
	return WithSkillAssets(skill, manager, sandbox.SessionAssets{})
}

// WithSkillAssets binds a skill, sandbox manager, and additional session assets to the runner.
func WithSkillAssets(skill *skills.Skill, manager *sandbox.Manager, assets sandbox.SessionAssets) RunnerOption {
	return func(r *Runner) {
		if skill != nil && manager != nil {
			r.skillRuntime = &skillRuntime{skill: skill, manager: manager, assets: assets}
		}
	}
}

// New creates a new runner.
func New(provider core.Provider, opts ...RunnerOption) *Runner {
	r := &Runner{
		provider:    provider,
		maxParallel: 5,
		toolTimeout: 30 * time.Second,
		errorMode:   ToolErrorPropagate,
		retry: ToolRetry{
			MaxAttempts: 1,
			BaseDelay:   250 * time.Millisecond,
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	if len(r.interceptors) == 0 {
		r.interceptors = []Interceptor{noopInterceptor{}}
	}
	return r
}

func (r *Runner) prepareSkillSession(ctx context.Context) (*sandbox.Session, func(), error) {
	if r.skillRuntime == nil {
		return nil, func() {}, nil
	}
	session, err := r.skillRuntime.manager.CreateSessionWithAssets(ctx, r.skillRuntime.skill.SessionSpec(), r.skillRuntime.assets)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		r.skillRuntime.manager.Destroy(session.ID)
	}
	if r.skillRuntime.skill.Manifest.Sandbox.Warm {
		if err := r.skillRuntime.skill.Prewarm(ctx, session); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	return session, cleanup, nil
}

// ExecuteRequest runs the request to completion, handling tool calls until stop conditions are met.
func (r *Runner) ExecuteRequest(ctx context.Context, req core.Request) (resp *core.TextResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	providerName := r.provider.Capabilities().Provider
	ctx, recorder := obs.StartRequest(ctx, "runner.ExecuteRequest",
		attribute.String("ai.provider", providerName),
		attribute.String("ai.runner", "gai"),
	)
	defer func() {
		usage := obs.UsageTokens{}
		if resp != nil {
			usage = obs.UsageFromCore(resp.Usage)
		}
		recorder.End(err, usage)
	}()

	stopWhen := req.StopWhen
	if stopWhen == nil {
		stopWhen = core.NoMoreTools()
	}

	state := &core.RunnerState{
		Messages: append([]core.Message(nil), req.Messages...),
		Steps:    []core.Step{},
		Usage:    core.Usage{},
	}

	skillSession, cleanup, err := r.prepareSkillSession(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	for {
		if ctx.Err() != nil {
			err = ctx.Err()
			return nil, err
		}

		if stop, reason := stopWhen(state); stop {
			if reason.Type == "" {
				reason.Type = core.StopReasonComplete
			}
			state.StopReason = reason
			break
		}

		step, stepErr := r.executeStep(ctx, req, state, nil, skillSession)
		if stepErr != nil {
			err = stepErr
			return nil, err
		}

		state.Steps = append(state.Steps, step)
		state.LastStep = &state.Steps[len(state.Steps)-1]
		state.Usage = addUsage(state.Usage, step.Usage)

		if len(step.ToolCalls) == 0 {
			if state.StopReason.Type == "" {
				state.StopReason = core.StopReason{Type: core.StopReasonNoMoreTools}
			}
			break
		}
	}

	if state.StopReason.Type == "" {
		state.StopReason = core.StopReason{Type: core.StopReasonComplete}
	}

	if req.OnStop != nil {
		return r.runFinalizer(ctx, req.OnStop, state)
	}

	resp = &core.TextResult{
		Text:         state.LastText(),
		Steps:        append([]core.Step(nil), state.Steps...),
		Usage:        state.Usage,
		FinishReason: state.StopReason,
		Provider:     providerName,
	}
	return resp, nil
}

// StreamRequest executes the request while streaming normalized events to the caller.
func (r *Runner) StreamRequest(ctx context.Context, req core.Request) (*core.Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	stream := core.NewStream(ctx, 128)
	go r.runStreamRequest(ctx, req, stream)
	return stream, nil
}

func (r *Runner) runStreamRequest(ctx context.Context, req core.Request, stream *core.Stream) {
	providerName := r.provider.Capabilities().Provider
	ctx, recorder := obs.StartRequest(ctx, "runner.StreamRequest",
		attribute.String("ai.provider", providerName),
		attribute.String("ai.runner", "gai"),
	)

	stopWhen := req.StopWhen
	if stopWhen == nil {
		stopWhen = core.NoMoreTools()
	}
	state := &core.RunnerState{
		Messages: append([]core.Message(nil), req.Messages...),
		Steps:    []core.Step{},
		Usage:    core.Usage{},
	}
	publisher := &streamPublisher{stream: stream, provider: providerName}
	publisher.emitRunnerStart()

	skillSession, cleanup, err := r.prepareSkillSession(ctx)
	if err != nil {
		recorder.End(err, obs.UsageTokens{})
		stream.Fail(err)
		return
	}
	defer cleanup()

	var (
		lastModel string
		runErr    error
	)

	for {
		if ctx.Err() != nil {
			runErr = ctx.Err()
			break
		}
		if stop, reason := stopWhen(state); stop {
			if reason.Type == "" {
				reason.Type = core.StopReasonComplete
			}
			state.StopReason = reason
			break
		}

		step, err := r.executeStep(ctx, req, state, publisher, skillSession)
		if err != nil {
			runErr = err
			break
		}
		state.Steps = append(state.Steps, step)
		state.LastStep = &state.Steps[len(state.Steps)-1]
		state.Usage = addUsage(state.Usage, step.Usage)
		lastModel = step.Model

		if len(step.ToolCalls) == 0 {
			if state.StopReason.Type == "" {
				state.StopReason = core.StopReason{Type: core.StopReasonNoMoreTools}
			}
			break
		}
	}

	if runErr == nil && req.OnStop != nil {
		prevUsage := state.Usage
		prevSteps := len(state.Steps)
		prevLastText := ""
		if prevSteps > 0 {
			prevLastText = strings.TrimSpace(state.Steps[prevSteps-1].Text)
		}
		res, err := r.runFinalizer(ctx, req.OnStop, state)
		if err != nil {
			runErr = err
		} else if res != nil {
			newSteps := append([]core.Step(nil), res.Steps...)
			if len(newSteps) < prevSteps {
				prevSteps = len(newSteps)
			}
			appended := []core.Step{}
			if len(newSteps) > prevSteps {
				appended = append(appended, newSteps[prevSteps:]...)
			}
			finalText := strings.TrimSpace(res.Text)
			stepUsage := subtractUsage(res.Usage, prevUsage)
			if len(appended) == 0 && finalText != "" && finalText != prevLastText {
				now := time.Now()
				modelForSynthetic := res.Model
				if modelForSynthetic == "" {
					if len(state.Steps) > 0 && state.Steps[len(state.Steps)-1].Model != "" {
						modelForSynthetic = state.Steps[len(state.Steps)-1].Model
					} else if req.Model != "" {
						modelForSynthetic = req.Model
					} else {
						modelForSynthetic = providerName
					}
				}
				synthetic := core.Step{
					Number:      prevSteps + 1,
					Text:        finalText,
					Usage:       stepUsage,
					DurationMS:  0,
					StartedAt:   now.UnixMilli(),
					CompletedAt: now.UnixMilli(),
					Model:       modelForSynthetic,
				}
				newSteps = append(newSteps, synthetic)
				appended = []core.Step{synthetic}
			}
			if publisher != nil && len(appended) > 0 {
				for _, step := range appended {
					publisher.emitStepStart(step.Number)
					if text := strings.TrimSpace(step.Text); text != "" {
						publisher.push(core.StreamEvent{Type: core.EventTextDelta, TextDelta: text}, step.Number)
					}
					publisher.emitStepFinish(step)
				}
			}
			state.Steps = append([]core.Step(nil), newSteps...)
			if len(state.Steps) > 0 {
				state.LastStep = &state.Steps[len(state.Steps)-1]
			} else {
				state.LastStep = nil
			}
			state.Usage = res.Usage
			state.StopReason = res.FinishReason
			if finalText != "" {
				latestText := strings.TrimSpace(state.LastText())
				if latestText == "" {
					latestText = finalText
				}
				if latestText != prevLastText {
					state.Messages = append(state.Messages, core.AssistantMessage(latestText))
				}
			}
			if res.Model != "" {
				lastModel = res.Model
			} else if state.LastStep != nil && state.LastStep.Model != "" {
				lastModel = state.LastStep.Model
			}
			if res.Provider != "" {
				providerName = res.Provider
				if publisher != nil {
					publisher.provider = providerName
				}
			}
		}
	}

	if runErr == nil {
		if state.StopReason.Type == "" {
			state.StopReason = core.StopReason{Type: core.StopReasonComplete}
		}
		publisher.emitRunnerFinish(state)
		stream.SetMeta(core.StreamMeta{Model: lastModel, Provider: providerName, Usage: state.Usage})
	}

	usage := obs.UsageTokens{}
	if runErr == nil {
		usage = obs.UsageFromCore(state.Usage)
	}
	recorder.End(runErr, usage)
	if runErr != nil {
		stream.Fail(runErr)
		return
	}
	_ = stream.Close()
}

func (r *Runner) executeStep(ctx context.Context, req core.Request, state *core.RunnerState, publisher *streamPublisher, session *sandbox.Session) (core.Step, error) {
	stepNum := len(state.Steps) + 1
	stepCtx := ctx
	for _, ic := range r.interceptors {
		stepCtx = ic.BeforeStep(stepCtx, req, state, stepNum)
	}
	spanCtx, span := obs.Tracer().Start(stepCtx, "runner.Step",
		trace.WithAttributes(
			attribute.Int("ai.runner.step", stepNum),
			attribute.String("ai.provider", r.provider.Capabilities().Provider),
		),
	)

	stepStart := time.Now()
	stream, err := r.provider.StreamText(spanCtx, core.Request{
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return core.Step{}, err
	}
	defer stream.Close()
	if publisher != nil {
		publisher.emitStepStart(stepNum)
	}

	builder := &strings.Builder{}
	toolCalls := make([]core.ToolCall, 0)
	usage := core.Usage{}
	var finishReason *core.StopReason

	for event := range stream.Events() {
		if publisher != nil {
			publisher.push(event, stepNum)
		}
		switch event.Type {
		case core.EventTextDelta:
			builder.WriteString(event.TextDelta)
		case core.EventToolCall:
			toolCalls = append(toolCalls, event.ToolCall)
		case core.EventFinish:
			usage = event.Usage
			if event.FinishReason != nil {
				finishReason = event.FinishReason
			}
		case core.EventError:
			if event.Error != nil {
				stream.Fail(event.Error)
				return core.Step{}, event.Error
			}
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, core.ErrStreamClosed) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return core.Step{}, err
	}
	if publisher != nil {
		publisher.addWarnings(stream.Warnings()...)
	}

	meta := stream.Meta()

	assistantText := builder.String()
	state.Messages = append(state.Messages, core.Message{Role: core.Assistant, Parts: []core.Part{core.Text{Text: assistantText}}})

	executions := make([]core.ToolExecution, 0, len(toolCalls))
	if len(toolCalls) > 0 {
		execs, err := r.invokeTools(stepCtx, toolCalls, req.Tools, stepNum, session)
		if err != nil && r.errorMode == ToolErrorPropagate {
			return core.Step{}, err
		}
		executions = execs
		state.Messages = append(state.Messages, buildToolResultMessages(executions)...)
		if publisher != nil {
			for _, exec := range executions {
				publisher.emitToolResult(stepNum, exec)
			}
		}
	}

	if finishReason != nil && finishReason.Type != "" {
		state.StopReason = *finishReason
	}

	finishedAt := time.Now()
	step := core.Step{
		Number:      stepNum,
		Text:        assistantText,
		ToolCalls:   executions,
		Usage:       usage,
		DurationMS:  finishedAt.Sub(stepStart).Milliseconds(),
		StartedAt:   stepStart.UnixMilli(),
		CompletedAt: finishedAt.UnixMilli(),
		Model:       meta.Model,
	}
	span.SetAttributes(
		attribute.Int64("ai.tokens.input", int64(usage.InputTokens)),
		attribute.Int64("ai.tokens.output", int64(usage.OutputTokens)),
		attribute.Int("ai.tools.invoked", len(executions)),
	)
	span.End()

	for _, ic := range r.interceptors {
		ic.AfterStep(stepCtx, step, state)
	}

	if publisher != nil {
		publisher.emitStepFinish(step)
	}

	return step, nil
}

func (r *Runner) invokeTools(ctx context.Context, calls []core.ToolCall, tools []core.ToolHandle, stepID int, session *sandbox.Session) ([]core.ToolExecution, error) {
	index := make(map[string]core.ToolHandle, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		index[tool.Name()] = tool
	}

	results := make([]core.ToolExecution, len(calls))
	var wg sync.WaitGroup
	errCh := make(chan error, len(calls))
	sem := make(chan struct{}, max(1, r.maxParallel))

	for i, call := range calls {
		handle, ok := index[call.Name]
		if !ok {
			err := fmt.Errorf("tool %s not registered", call.Name)
			results[i] = core.ToolExecution{Call: call, Error: err}
			if r.errorMode == ToolErrorPropagate {
				errCh <- err
			}
			continue
		}

		wg.Add(1)
		go func(idx int, c core.ToolCall, h core.ToolHandle) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = core.ToolExecution{Call: c, Error: ctx.Err()}
				if r.errorMode == ToolErrorPropagate {
					errCh <- ctx.Err()
				}
				return
			}
			defer func() { <-sem }()

			meta := &core.ToolMeta{CallID: c.ID, StepID: stepID, Metadata: map[string]any{}}
			if session != nil {
				meta.Metadata["sandbox.session"] = session
			}
			execCtx := ctx
			for _, ic := range r.interceptors {
				execCtx = ic.BeforeTool(execCtx, c, meta)
			}

			exec, err := r.executeTool(execCtx, h, c, meta)
			if err != nil && r.errorMode == ToolErrorPropagate {
				errCh <- err
			}

			for _, ic := range r.interceptors {
				ic.AfterTool(execCtx, &exec, meta)
			}

			results[idx] = exec
		}(i, call, handle)
	}

	wg.Wait()
	close(errCh)

	var aggErr error
	for e := range errCh {
		if e != nil {
			aggErr = e
			break
		}
	}

	return results, aggErr
}

func (r *Runner) executeTool(ctx context.Context, handle core.ToolHandle, call core.ToolCall, meta *core.ToolMeta) (core.ToolExecution, error) {
	maxAttempts := max(1, r.retry.MaxAttempts)
	inputHash := toolInputHash(call.Input)

	if r.memo != nil {
		if cached, ok := r.memo.Get(call.Name, inputHash); ok {
			return core.ToolExecution{Call: call, Result: cached}, nil
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return core.ToolExecution{Call: call, Error: ctx.Err(), Retries: attempt}, ctx.Err()
		}

		attrs := []attribute.KeyValue{
			attribute.String("ai.tool.name", handle.Name()),
			attribute.String("ai.tool.call_id", call.ID),
			attribute.Int("ai.tool.retry_attempt", attempt),
		}
		attemptCtx, cancel := context.WithTimeout(ctx, r.toolTimeout)
		toolCtx, span := obs.Tracer().Start(attemptCtx, "runner.Tool",
			trace.WithAttributes(attrs...))
		start := time.Now()
		var metaVal core.ToolMeta
		if meta != nil {
			if meta.Metadata == nil {
				meta.Metadata = map[string]any{}
			}
			meta.Metadata["retry_attempt"] = attempt
			metaVal = *meta
		}
		result, execErr := handle.Execute(toolCtx, call.Input, metaVal)
		cancel()
		if execErr != nil {
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
		}
		duration := time.Since(start).Milliseconds()

		exec := core.ToolExecution{
			Call:       call,
			Result:     result,
			Error:      execErr,
			DurationMS: duration,
			Retries:    attempt,
		}
		span.End()

		if execErr == nil {
			if r.memo != nil {
				r.memo.Set(call.Name, inputHash, result)
			}
			return exec, nil
		}

		lastErr = execErr
		if attempt == maxAttempts-1 {
			return exec, execErr
		}

		delay := r.nextRetryDelay(attempt)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return core.ToolExecution{Call: call, Error: ctx.Err(), Retries: attempt + 1}, ctx.Err()
		}
	}

	return core.ToolExecution{Call: call, Error: lastErr, Retries: maxAttempts - 1}, lastErr
}

func (r *Runner) nextRetryDelay(attempt int) time.Duration {
	base := r.retry.BaseDelay
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	delay := base * time.Duration(math.Pow(2, float64(attempt)))
	if max := r.retry.MaxDelay; max > 0 && delay > max {
		delay = max
	}
	if r.retry.Jitter {
		jitterMu.Lock()
		factor := 0.5 + jitterRand.Float64()
		jitterMu.Unlock()
		delay = time.Duration(float64(delay) * factor)
	}
	return delay
}

func (r *Runner) runFinalizer(ctx context.Context, finalizer core.Finalizer, state *core.RunnerState) (*core.TextResult, error) {
	if finalizer == nil {
		return &core.TextResult{
			Text:         state.LastText(),
			Steps:        append([]core.Step(nil), state.Steps...),
			Usage:        state.Usage,
			FinishReason: state.StopReason,
			Provider:     r.provider.Capabilities().Provider,
		}, nil
	}

	finalState := core.FinalState{
		Messages:    append([]core.Message(nil), state.Messages...),
		Steps:       append([]core.Step(nil), state.Steps...),
		Usage:       state.Usage,
		StopReason:  state.StopReason,
		LastText:    func() string { return state.LastText() },
		TotalTokens: func() int { return state.Usage.TotalTokens },
	}

	return finalizer(ctx, finalState)
}

func buildToolResultMessages(executions []core.ToolExecution) []core.Message {
	messages := make([]core.Message, 0, len(executions)*2)
	for _, exec := range executions {
		if exec.Call.Name == "" {
			continue
		}
		callInput := map[string]any{}
		for k, v := range exec.Call.Input {
			callInput[k] = v
		}
		callMetadata := map[string]any{}
		for k, v := range exec.Call.Metadata {
			callMetadata[k] = v
		}
		callPart := core.ToolCall{
			ID:       exec.Call.ID,
			Name:     exec.Call.Name,
			Input:    callInput,
			Metadata: callMetadata,
		}
		callMessage := core.Message{Role: core.Assistant, Parts: []core.Part{callPart}}
		if len(callMetadata) > 0 {
			callMessage.Metadata = copyAnyMap(callMetadata)
		}
		messages = append(messages, callMessage)
		if exec.Error != nil {
			messages = append(messages, core.Message{Role: core.User, Parts: []core.Part{core.ToolResult{ID: exec.Call.ID, Name: exec.Call.Name, Error: exec.Error.Error()}}})
			continue
		}
		messages = append(messages, core.Message{Role: core.User, Parts: []core.Part{core.ToolResult{ID: exec.Call.ID, Name: exec.Call.Name, Result: exec.Result}}})
	}
	return messages
}

func copyAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func addUsage(total, step core.Usage) core.Usage {
	total.InputTokens += step.InputTokens
	total.OutputTokens += step.OutputTokens
	total.TotalTokens += step.TotalTokens
	total.ReasoningTokens += step.ReasoningTokens
	total.CachedInputTokens += step.CachedInputTokens
	total.AudioTokens += step.AudioTokens
	total.CostUSD += step.CostUSD
	return total
}

func subtractUsage(total, prior core.Usage) core.Usage {
	diff := core.Usage{
		InputTokens:       total.InputTokens - prior.InputTokens,
		OutputTokens:      total.OutputTokens - prior.OutputTokens,
		ReasoningTokens:   total.ReasoningTokens - prior.ReasoningTokens,
		TotalTokens:       total.TotalTokens - prior.TotalTokens,
		CachedInputTokens: total.CachedInputTokens - prior.CachedInputTokens,
		AudioTokens:       total.AudioTokens - prior.AudioTokens,
		CostUSD:           total.CostUSD - prior.CostUSD,
	}
	if diff.InputTokens < 0 {
		diff.InputTokens = 0
	}
	if diff.OutputTokens < 0 {
		diff.OutputTokens = 0
	}
	if diff.ReasoningTokens < 0 {
		diff.ReasoningTokens = 0
	}
	if diff.TotalTokens < 0 {
		diff.TotalTokens = 0
	}
	if diff.CachedInputTokens < 0 {
		diff.CachedInputTokens = 0
	}
	if diff.AudioTokens < 0 {
		diff.AudioTokens = 0
	}
	if diff.CostUSD < 0 {
		diff.CostUSD = 0
	}
	return diff
}

func toolInputHash(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	payload, err := json.Marshal(input)
	if err != nil {
		// Fallback to fmt for unserializable inputs
		payload = []byte(fmt.Sprint(input))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

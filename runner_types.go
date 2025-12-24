package gai

import (
	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/runner"
)

// Runner configuration types re-exported for convenience.
// These allow users to configure the runner without importing the runner package.

// ToolRetry configures retry behavior for tool executions.
// Used with WithToolRetry client option.
type ToolRetry = runner.ToolRetry

// ToolMemo provides caching for tool results.
// Implementations store and retrieve tool results based on name and input hash.
// Used with WithMemo client option.
type ToolMemo = runner.ToolMemo

// Interceptor hooks into runner lifecycle events.
// Use this for logging, metrics, or custom behavior during agentic execution.
// Used with WithInterceptor client option.
type Interceptor = runner.Interceptor

// ToolErrorMode controls error handling during tool execution.
type ToolErrorMode = runner.ToolErrorMode

// ToolErrorMode constants.
const (
	// ToolErrorPropagate stops execution on first tool error (default).
	ToolErrorPropagate = runner.ToolErrorPropagate

	// ToolErrorAppendAndContinue adds error to results and continues execution.
	ToolErrorAppendAndContinue = runner.ToolErrorAppendAndContinue
)

// Execution state types re-exported for convenience.
// These are used with stop conditions and finalizers.

// FinalState is passed to Finalizer callbacks after execution completes.
type FinalState = core.FinalState

// Finalizer is called after agentic execution completes.
// Use with OnStop() on the request builder.
type Finalizer = core.Finalizer

// Step represents a single step in multi-step execution.
type Step = core.Step

// ToolExecution captures a single tool invocation and its outcome.
type ToolExecution = core.ToolExecution

// Note: ToolMeta is already exported in types.go

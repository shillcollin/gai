package gai

import "github.com/shillcollin/gai/core"

// StopCondition determines whether the runner should halt execution.
// This is a type alias for core.StopCondition for convenience.
type StopCondition = core.StopCondition

// Stop condition factory functions.
// These are re-exports of core functions for convenience so users don't need
// to import both gai and gai/core packages.

// NoMoreTools stops when the last completed step did not execute any tools.
// This is useful for agentic loops where the model should continue until
// it has all the information it needs.
func NoMoreTools() StopCondition {
	return core.NoMoreTools()
}

// MaxSteps stops execution when the total number of steps reaches n.
// This prevents runaway loops and controls cost.
func MaxSteps(n int) StopCondition {
	return core.MaxSteps(n)
}

// UntilToolSeen stops after the named tool has been invoked at least once.
// This is useful when waiting for a specific action to complete.
func UntilToolSeen(name string) StopCondition {
	return core.UntilToolSeen(name)
}

// UntilTextLength stops once the accumulated assistant text meets the threshold.
func UntilTextLength(n int) StopCondition {
	return core.UntilTextLength(n)
}

// Any returns a condition that triggers when any of the provided conditions fire.
func Any(conds ...StopCondition) StopCondition {
	return core.Any(conds...)
}

// All returns a condition that triggers only when all provided conditions succeed.
func All(conds ...StopCondition) StopCondition {
	return core.All(conds...)
}

// MaxTokens stops when total token usage reaches the threshold.
// This helps control costs by limiting the total tokens consumed across all steps.
func MaxTokens(n int) StopCondition {
	return core.MaxTokens(n)
}

// MaxCost stops when total cost reaches the threshold in USD.
// This helps control spending by limiting the total cost across all steps.
func MaxCost(usd float64) StopCondition {
	return core.MaxCost(usd)
}

// Custom allows user-defined stop conditions.
// The provided function receives the current runner state and returns whether
// to stop and an optional reason.
//
// Example:
//
//	gai.Custom(func(state *gai.RunnerState) (bool, gai.StopReason) {
//	    if len(state.Steps) > 0 && strings.Contains(state.LastText(), "DONE") {
//	        return true, gai.StopReason{Type: "keyword_found", Description: "Found DONE"}
//	    }
//	    return false, gai.StopReason{}
//	})
func Custom(fn func(state *RunnerState) (stop bool, reason StopReason)) StopCondition {
	return core.Custom(fn)
}

// StopReason documents why generation ended.
// This is a type alias for core.StopReason for convenience.
type StopReason = core.StopReason

// RunnerState holds the mutable state of a multi-step execution.
// This is exposed for use with Custom stop conditions.
type RunnerState = core.RunnerState

// Stop reason type constants.
const (
	StopReasonUnknown        = core.StopReasonUnknown
	StopReasonComplete       = core.StopReasonComplete
	StopReasonMaxSteps       = core.StopReasonMaxSteps
	StopReasonMaxTokens      = core.StopReasonMaxTokens
	StopReasonMaxCost        = core.StopReasonMaxCost
	StopReasonNoMoreTools    = core.StopReasonNoMoreTools
	StopReasonToolSeen       = core.StopReasonToolSeen
	StopReasonTextLength     = core.StopReasonTextLength
	StopReasonKeywordFound   = core.StopReasonKeywordFound
	StopReasonAllConditions  = core.StopReasonAllConditions
	StopReasonToolError      = core.StopReasonToolError
	StopReasonProviderFinish = core.StopReasonProviderFinish
	StopReasonCustom         = core.StopReasonCustom
)

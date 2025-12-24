package core

import "fmt"

// MaxSteps stops execution when the total number of steps reaches n.
func MaxSteps(n int) StopCondition {
	if n <= 0 {
		n = 1
	}
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		if state.TotalSteps() >= n {
			return true, StopReason{
				Type:        StopReasonMaxSteps,
				Description: fmt.Sprintf("reached maximum of %d steps", n),
			}
		}
		return false, StopReason{}
	}
}

// NoMoreTools stops when the last completed step did not execute any tools.
func NoMoreTools() StopCondition {
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil || state.LastStep == nil {
			return false, StopReason{}
		}
		if len(state.LastStep.ToolCalls) == 0 {
			return true, StopReason{
				Type:        StopReasonNoMoreTools,
				Description: "no tool calls in last step",
			}
		}
		return false, StopReason{}
	}
}

// UntilToolSeen stops after the named tool has been invoked at least once.
func UntilToolSeen(name string) StopCondition {
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		for _, step := range state.Steps {
			for _, call := range step.ToolCalls {
				if call.Call.Name == name {
					return true, StopReason{
						Type:        StopReasonToolSeen,
						Description: fmt.Sprintf("tool %s was called", name),
					}
				}
			}
		}
		return false, StopReason{}
	}
}

// UntilTextLength stops once the accumulated assistant text meets the threshold.
func UntilTextLength(n int) StopCondition {
	if n <= 0 {
		n = 1
	}
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		total := 0
		for _, step := range state.Steps {
			total += len(step.Text)
		}
		if state.LastStep != nil {
			total += len(state.LastStep.Text)
		}
		if total >= n {
			return true, StopReason{
				Type:        StopReasonTextLength,
				Description: fmt.Sprintf("assistant text length reached %d", total),
			}
		}
		return false, StopReason{}
	}
}

// Any returns a condition that triggers when any of the provided conditions fire.
func Any(conds ...StopCondition) StopCondition {
	return func(state *RunnerState) (bool, StopReason) {
		for _, cond := range conds {
			if cond == nil {
				continue
			}
			if stop, reason := cond(state); stop {
				return true, reason
			}
		}
		return false, StopReason{}
	}
}

// All returns a condition that triggers only when all provided conditions succeed.
func All(conds ...StopCondition) StopCondition {
	return func(state *RunnerState) (bool, StopReason) {
		if len(conds) == 0 {
			return false, StopReason{}
		}
		reasons := make([]string, 0, len(conds))
		for _, cond := range conds {
			if cond == nil {
				return false, StopReason{}
			}
			stop, reason := cond(state)
			if !stop {
				return false, StopReason{}
			}
			reasons = append(reasons, reason.Type)
		}
		return true, StopReason{
			Type:        StopReasonAllConditions,
			Description: fmt.Sprintf("all stop conditions met: %v", reasons),
		}
	}
}

// CombineConditions is an alias for Any to match the Developer Guide examples.
func CombineConditions(conds ...StopCondition) StopCondition {
	return Any(conds...)
}

// MaxTokens stops when total token usage reaches the threshold.
// This helps control costs by limiting the total tokens consumed across all steps.
func MaxTokens(n int) StopCondition {
	if n <= 0 {
		n = 1
	}
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		if state.Usage.TotalTokens >= n {
			return true, StopReason{
				Type:        StopReasonMaxTokens,
				Description: fmt.Sprintf("total tokens reached %d (limit: %d)", state.Usage.TotalTokens, n),
			}
		}
		return false, StopReason{}
	}
}

// MaxCost stops when total cost reaches the threshold in USD.
// This helps control spending by limiting the total cost across all steps.
func MaxCost(usd float64) StopCondition {
	if usd <= 0 {
		usd = 0.01 // minimum 1 cent
	}
	return func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		if state.Usage.CostUSD >= usd {
			return true, StopReason{
				Type:        StopReasonMaxCost,
				Description: fmt.Sprintf("cost reached $%.4f (limit: $%.4f)", state.Usage.CostUSD, usd),
			}
		}
		return false, StopReason{}
	}
}

// Custom allows user-defined stop conditions.
// The provided function receives the current runner state and returns whether
// to stop and an optional reason.
func Custom(fn func(state *RunnerState) (stop bool, reason StopReason)) StopCondition {
	if fn == nil {
		return func(*RunnerState) (bool, StopReason) {
			return false, StopReason{}
		}
	}
	return func(state *RunnerState) (bool, StopReason) {
		stop, reason := fn(state)
		if stop && reason.Type == "" {
			reason.Type = StopReasonCustom
		}
		return stop, reason
	}
}

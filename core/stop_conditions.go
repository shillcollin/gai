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

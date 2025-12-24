package gai

import (
	"testing"

	"github.com/shillcollin/gai/core"
)

func TestNoMoreToolsReexport(t *testing.T) {
	cond := NoMoreTools()
	if cond == nil {
		t.Fatal("NoMoreTools() returned nil")
	}

	// Test that it works correctly
	state := &core.RunnerState{
		LastStep: &core.Step{
			ToolCalls: nil, // no tool calls
		},
	}

	stop, reason := cond(state)
	if !stop {
		t.Error("expected NoMoreTools to stop when no tool calls")
	}
	if reason.Type != StopReasonNoMoreTools {
		t.Errorf("expected StopReasonNoMoreTools, got %s", reason.Type)
	}
}

func TestMaxStepsReexport(t *testing.T) {
	cond := MaxSteps(3)
	if cond == nil {
		t.Fatal("MaxSteps() returned nil")
	}

	// Test that it doesn't stop before max
	state := &core.RunnerState{
		Steps: []core.Step{{}, {}}, // 2 steps
	}
	stop, _ := cond(state)
	if stop {
		t.Error("MaxSteps(3) should not stop at 2 steps")
	}

	// Test that it stops at max
	state.Steps = append(state.Steps, core.Step{})
	stop, reason := cond(state)
	if !stop {
		t.Error("MaxSteps(3) should stop at 3 steps")
	}
	if reason.Type != StopReasonMaxSteps {
		t.Errorf("expected StopReasonMaxSteps, got %s", reason.Type)
	}
}

func TestAnyConditionReexport(t *testing.T) {
	cond := Any(
		MaxSteps(5),
		NoMoreTools(),
	)
	if cond == nil {
		t.Fatal("Any() returned nil")
	}

	// Should stop when NoMoreTools triggers
	state := &core.RunnerState{
		Steps:    []core.Step{{}},
		LastStep: &core.Step{ToolCalls: nil},
	}
	stop, _ := cond(state)
	if !stop {
		t.Error("Any() should stop when one condition is met")
	}
}

func TestAllConditionReexport(t *testing.T) {
	cond := All(
		MaxSteps(5),
		NoMoreTools(),
	)
	if cond == nil {
		t.Fatal("All() returned nil")
	}

	// Should not stop when only one condition is met
	state := &core.RunnerState{
		Steps: []core.Step{{}},
		LastStep: &core.Step{
			ToolCalls: []core.ToolExecution{{Call: core.ToolCall{Name: "test"}}},
		},
	}
	stop, _ := cond(state)
	if stop {
		t.Error("All() should not stop when only one condition is met")
	}
}

func TestStopReasonConstants(t *testing.T) {
	// Verify constants are properly re-exported
	if StopReasonUnknown != "unknown" {
		t.Errorf("StopReasonUnknown mismatch: got %q", StopReasonUnknown)
	}
	if StopReasonComplete != "complete" {
		t.Errorf("StopReasonComplete mismatch: got %q", StopReasonComplete)
	}
	if StopReasonMaxSteps != "max_steps" {
		t.Errorf("StopReasonMaxSteps mismatch: got %q", StopReasonMaxSteps)
	}
	if StopReasonNoMoreTools != "no_more_tools" {
		t.Errorf("StopReasonNoMoreTools mismatch: got %q", StopReasonNoMoreTools)
	}
	if StopReasonMaxTokens != "max_tokens" {
		t.Errorf("StopReasonMaxTokens mismatch: got %q", StopReasonMaxTokens)
	}
	if StopReasonMaxCost != "max_cost" {
		t.Errorf("StopReasonMaxCost mismatch: got %q", StopReasonMaxCost)
	}
	if StopReasonCustom != "custom" {
		t.Errorf("StopReasonCustom mismatch: got %q", StopReasonCustom)
	}
}

func TestMaxTokensReexport(t *testing.T) {
	cond := MaxTokens(100)
	if cond == nil {
		t.Fatal("MaxTokens() returned nil")
	}

	// Test that it doesn't stop before max
	state := &core.RunnerState{
		Usage: core.Usage{TotalTokens: 50},
	}
	stop, _ := cond(state)
	if stop {
		t.Error("MaxTokens(100) should not stop at 50 tokens")
	}

	// Test that it stops at max
	state.Usage.TotalTokens = 100
	stop, reason := cond(state)
	if !stop {
		t.Error("MaxTokens(100) should stop at 100 tokens")
	}
	if reason.Type != StopReasonMaxTokens {
		t.Errorf("expected StopReasonMaxTokens, got %s", reason.Type)
	}
}

func TestMaxCostReexport(t *testing.T) {
	cond := MaxCost(0.50)
	if cond == nil {
		t.Fatal("MaxCost() returned nil")
	}

	// Test that it doesn't stop before max
	state := &core.RunnerState{
		Usage: core.Usage{CostUSD: 0.25},
	}
	stop, _ := cond(state)
	if stop {
		t.Error("MaxCost(0.50) should not stop at $0.25")
	}

	// Test that it stops at max
	state.Usage.CostUSD = 0.50
	stop, reason := cond(state)
	if !stop {
		t.Error("MaxCost(0.50) should stop at $0.50")
	}
	if reason.Type != StopReasonMaxCost {
		t.Errorf("expected StopReasonMaxCost, got %s", reason.Type)
	}
}

func TestCustomReexport(t *testing.T) {
	// Custom condition that stops when we've seen 2 steps
	cond := Custom(func(state *RunnerState) (bool, StopReason) {
		if state == nil {
			return false, StopReason{}
		}
		if len(state.Steps) >= 2 {
			return true, StopReason{
				Type:        "custom_two_steps",
				Description: "stopped after 2 steps",
			}
		}
		return false, StopReason{}
	})

	if cond == nil {
		t.Fatal("Custom() returned nil")
	}

	// Test that it doesn't stop before 2 steps
	state := &core.RunnerState{
		Steps: []core.Step{{}},
	}
	stop, _ := cond(state)
	if stop {
		t.Error("Custom condition should not stop at 1 step")
	}

	// Test that it stops at 2 steps
	state.Steps = append(state.Steps, core.Step{})
	stop, reason := cond(state)
	if !stop {
		t.Error("Custom condition should stop at 2 steps")
	}
	if reason.Type != "custom_two_steps" {
		t.Errorf("expected custom_two_steps, got %s", reason.Type)
	}
}

func TestCustomNilFunction(t *testing.T) {
	// Custom with nil function should return a no-op condition
	cond := Custom(nil)
	if cond == nil {
		t.Fatal("Custom(nil) returned nil")
	}

	state := &core.RunnerState{
		Steps: []core.Step{{}, {}, {}},
	}
	stop, _ := cond(state)
	if stop {
		t.Error("Custom(nil) should never stop")
	}
}

func TestCustomWithEmptyReason(t *testing.T) {
	// Custom condition that returns empty reason type should get "custom" type assigned
	cond := Custom(func(state *RunnerState) (bool, StopReason) {
		return true, StopReason{Description: "stopping now"}
	})

	state := &core.RunnerState{}
	stop, reason := cond(state)
	if !stop {
		t.Error("Custom condition should stop")
	}
	if reason.Type != StopReasonCustom {
		t.Errorf("expected StopReasonCustom for empty type, got %s", reason.Type)
	}
}

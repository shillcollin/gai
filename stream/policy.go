package stream

import (
	"maps"

	"github.com/shillcollin/gai/core"
)

// Policy controls which events are emitted and how sensitive data is handled.
type Policy struct {
	SendStart     bool
	SendFinish    bool
	SendReasoning bool
	SendSources   bool
	MaskErrors    bool
	BufferSize    int
}

func (p Policy) isZero() bool {
	return p == Policy{}
}

func filterEvent(policy Policy, event core.StreamEvent) (core.StreamEvent, bool) {
	if policy.isZero() {
		return event, true
	}
	switch event.Type {
	case core.EventStart:
		if !policy.SendStart {
			return core.StreamEvent{}, false
		}
	case core.EventFinish:
		if !policy.SendFinish {
			return core.StreamEvent{}, false
		}
	case core.EventReasoningDelta, core.EventReasoningSummary:
		if !policy.SendReasoning {
			return core.StreamEvent{}, false
		}
	case core.EventCitations:
		if !policy.SendSources {
			return core.StreamEvent{}, false
		}
	}
	// Return a shallow copy to avoid mutating underlying stream state when masking.
	cloned := event
	if event.Ext != nil {
		cloned.Ext = maps.Clone(event.Ext)
	}
	if event.Capabilities != nil {
		cloned.Capabilities = append([]string(nil), event.Capabilities...)
	}
	return cloned, true
}

func maskError(event core.StreamEvent) core.StreamEvent {
	if event.Type != core.EventError {
		return event
	}
	if event.Ext == nil {
		event.Ext = map[string]any{}
	}
	event.Ext["error_masked"] = true
	return event
}

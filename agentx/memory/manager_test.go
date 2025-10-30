package memory

import (
	"context"
	"testing"

	"github.com/shillcollin/gai/core"
)

type stubProvider struct {
	entries []Entry
}

func (s stubProvider) Query(context.Context, Scope, Query) ([]Entry, error) {
	return append([]Entry(nil), s.entries...), nil
}

func TestBuildPinned(t *testing.T) {
	mgr := NewManager(Strategy{
		Pinned: PinnedStrategy{Enabled: true, Template: "Pinned:\n{{content}}"},
	}, stubProvider{entries: []Entry{{Title: "Fact", Content: "Value"}}}, Scope{})

	msgs, err := mgr.BuildPinned(context.Background())
	if err != nil {
		t.Fatalf("build pinned: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != core.System {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
	if got := msgs[0].Parts[0].(core.Text).Text; got == "" {
		t.Fatalf("expected content, got empty")
	}
}

func TestBuildPhaseDedupe(t *testing.T) {
	mgr := NewManager(Strategy{
		Phases: map[string]PhaseStrategy{
			"plan": {Queries: []Query{{Name: "facts", TopK: 2}}},
		},
		Dedupe: DedupeStrategy{ByTitle: true},
	}, stubProvider{entries: []Entry{{Title: "A", Content: "One"}, {Title: "A", Content: "Duplicate"}}}, Scope{})

	msgs, err := mgr.BuildPhase(context.Background(), "plan")
	if err != nil {
		t.Fatalf("build phase: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one message")
	}
}

package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shillcollin/gai/core"
)

// Strategy defines how memory should be injected.
type Strategy struct {
	Pinned PinnedStrategy
	Phases map[string]PhaseStrategy
	Redact bool
	Dedupe DedupeStrategy
}

// PinnedStrategy controls a persistent block injected near the persona.
type PinnedStrategy struct {
	Enabled   bool
	MaxTokens int
	Template  string
}

// PhaseStrategy describes per-phase retrieval behaviour.
type PhaseStrategy struct {
	Queries []Query
}

// Query models a memory lookup request.
type Query struct {
	Name      string
	Filter    map[string]any
	TopK      int
	MaxTokens int
}

// DedupeStrategy specifies deduplication knobs.
type DedupeStrategy struct {
	ByHash  bool
	ByTitle bool
}

// Entry represents a single compiled memory snippet returned by a provider.
type Entry struct {
	Title    string
	Content  string
	Tokens   int
	Metadata map[string]any
}

// Provider exposes generic memory operations used by Manager.
type Provider interface {
	Query(ctx context.Context, scope Scope, q Query) ([]Entry, error)
}

// Scope identifies the memory subject (agent/user/org/etc.).
type Scope struct {
	Kind string
	ID   string
}

// Manager compiles pinned and phase-specific memory blocks.
type Manager struct {
	Strategy Strategy
	Provider Provider
	Scope    Scope
}

// NewManager constructs a memory manager.
func NewManager(strategy Strategy, provider Provider, scope Scope) *Manager {
	if strategy.Phases == nil {
		strategy.Phases = map[string]PhaseStrategy{}
	}
	return &Manager{Strategy: strategy, Provider: provider, Scope: scope}
}

// BuildPinned compiles pinned memory messages.
func (m *Manager) BuildPinned(ctx context.Context) ([]core.Message, error) {
	if !m.Strategy.Pinned.Enabled {
		return nil, nil
	}
	entries, err := m.fetchEntries(ctx, Query{Name: "pinned", TopK: 1, MaxTokens: m.Strategy.Pinned.MaxTokens})
	if err != nil {
		return nil, err
	}
	text := compileEntries(entries, m.Strategy.Pinned.Template)
	if text == "" {
		return nil, nil
	}
	return []core.Message{core.SystemMessage(text)}, nil
}

// BuildPhase compiles phase-specific memory.
func (m *Manager) BuildPhase(ctx context.Context, phase string) ([]core.Message, error) {
	strat, ok := m.Strategy.Phases[strings.ToLower(phase)]
	if !ok {
		return nil, nil
	}
	var blobs []string
	for _, q := range strat.Queries {
		entries, err := m.fetchEntries(ctx, q)
		if err != nil {
			return nil, err
		}
		if m.Strategy.Dedupe.ByHash {
			entries = dedupeBy(entries, func(e Entry) string {
				if v, ok := e.Metadata["hash"].(string); ok {
					return v
				}
				return fmt.Sprintf("%s:%s", e.Title, e.Content)
			})
		}
		if m.Strategy.Dedupe.ByTitle {
			entries = dedupeBy(entries, func(e Entry) string { return e.Title })
		}
		if len(entries) == 0 {
			continue
		}
		blobs = append(blobs, compileEntries(entries, ""))
	}
	if len(blobs) == 0 {
		return nil, nil
	}
	text := strings.Join(blobs, "\n\n")
	return []core.Message{core.SystemMessage(text)}, nil
}

func (m *Manager) fetchEntries(ctx context.Context, q Query) ([]Entry, error) {
	if m.Provider == nil {
		return nil, nil
	}
	entries, err := m.Provider.Query(ctx, m.Scope, q)
	if err != nil {
		return nil, err
	}
	if q.TopK > 0 && len(entries) > q.TopK {
		entries = entries[:q.TopK]
	}
	return entries, nil
}

func compileEntries(entries []Entry, template string) string {
	if len(entries) == 0 {
		return ""
	}
	if strings.TrimSpace(template) == "" {
		lines := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.Title != "" {
				lines = append(lines, fmt.Sprintf("%s\n%s", e.Title, e.Content))
			} else {
				lines = append(lines, e.Content)
			}
		}
		return strings.Join(lines, "\n\n")
	}
	// Simple templating: replace {{content}} with compiled blocks.
	compiled := compileEntries(entries, "")
	return strings.ReplaceAll(template, "{{content}}", compiled)
}

func dedupeBy(entries []Entry, keyFn func(Entry) string) []Entry {
	seen := make(map[string]struct{})
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		key := keyFn(e)
		if key == "" {
			key = e.Content
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	return out
}

// NullProvider returns no memory entries; useful defaults.
type NullProvider struct{}

// Query implements Provider.
func (NullProvider) Query(context.Context, Scope, Query) ([]Entry, error) {
	return nil, nil
}

// Validate ensures strategy configuration is sane.
func (s Strategy) Validate() error {
	if s.Pinned.Enabled && s.Pinned.MaxTokens < 0 {
		return errors.New("pinned max tokens cannot be negative")
	}
	for name, phase := range s.Phases {
		if len(phase.Queries) == 0 {
			return fmt.Errorf("phase %s has no queries", name)
		}
	}
	return nil
}

package agentx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shillcollin/gai/agentx/approvals"
	"github.com/shillcollin/gai/agentx/plan"
	"github.com/shillcollin/gai/core"
)

// requestPlanApproval submits a plan approval request and waits for decision.
func requestPlanApproval(lc *LoopContext, planPath, planSig string) error {
	req := approvals.Request{
		Kind:      approvals.KindPlan,
		PlanPath:  relPath(lc.task.Progress("."), planPath),
		PlanSig:   planSig,
		Rationale: fmt.Sprintf("Plan approval for goal: %s", lc.spec.Goal),
		ExpiresAt: time.Now().UTC().Add(2 * time.Hour),
	}

	return lc.RequestApproval(req)
}

// prepareToolHandles filters and wraps tools based on plan and approval policy.
func prepareToolHandles(lc *LoopContext, p *plan.PlanV1) ([]core.ToolHandle, error) {
	var allowed map[string]struct{}
	if p != nil && len(p.AllowedTools) > 0 {
		allowed = make(map[string]struct{})
		for _, name := range p.AllowedTools {
			allowed[name] = struct{}{}
		}
	}

	requireSet := make(map[string]struct{})
	for _, name := range lc.agent.opts.Approvals.RequireTools {
		requireSet[name] = struct{}{}
	}

	var handles []core.ToolHandle
	for name, handle := range lc.agent.toolIndex {
		// Skip if plan restricts tools and this isn't allowed
		if allowed != nil {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}

		// Wrap with approval handler if required
		if _, requireApproval := requireSet[name]; requireApproval {
			handles = append(handles, &approvalToolHandle{
				ToolHandle: handle,
				lc:         lc,
			})
		} else {
			handles = append(handles, handle)
		}
	}

	sort.Slice(handles, func(i, j int) bool {
		return handles[i].Name() < handles[j].Name()
	})

	return handles, nil
}

// approvalToolHandle wraps a tool to enforce approval before execution.
type approvalToolHandle struct {
	core.ToolHandle
	lc *LoopContext
}

func (h *approvalToolHandle) Execute(ctx context.Context, input map[string]any, meta core.ToolMeta) (any, error) {
	// Request approval
	req := approvals.Request{
		Kind:      approvals.KindTool,
		ToolName:  h.Name(),
		Rationale: fmt.Sprintf("Tool %s invocation", h.Name()),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		Metadata:  map[string]any{"input": input},
	}

	if err := h.lc.RequestApproval(req); err != nil {
		return nil, fmt.Errorf("tool %s approval denied: %w", h.Name(), err)
	}

	// Execute tool
	return h.ToolHandle.Execute(ctx, input, meta)
}

// writeAtomic writes data to a file atomically using temp + rename.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

// relPath computes a relative path from base to target.
func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

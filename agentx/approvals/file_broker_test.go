package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shillcollin/gai/agentx/plan"
)

func TestFileBrokerSubmitAndAwait(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	planPath := filepath.Join(planDir, "current.json")
	p := plan.PlanV1{
		Goal:         "goal",
		Steps:        []string{"step"},
		AllowedTools: []string{"tool"},
	}
	sig, err := p.Signature()
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	payload, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(planPath, payload, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	broker, err := NewFileBroker(dir)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}

	req := Request{Kind: KindPlan, PlanPath: "plan/current.json", PlanSig: sig}
	id, err := broker.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Simulate approver writing decision after slight delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		dec := Decision{ID: id, Status: StatusApproved, ApprovedBy: "tester", Timestamp: time.Now().UTC()}
		payload, _ := json.MarshalIndent(dec, "", "  ")
		os.WriteFile(filepath.Join(dir, "approvals", "decisions", id+".json"), payload, 0o644)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dec, err := broker.AwaitDecision(ctx, id)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if dec.Status != StatusApproved {
		t.Fatalf("unexpected status %s", dec.Status)
	}
}

func TestFileBrokerStalePlan(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "plan")
	os.MkdirAll(planDir, 0o755)
	planPath := filepath.Join(planDir, "current.json")

	p := plan.PlanV1{Goal: "goal", Steps: []string{"step"}, AllowedTools: []string{"tool"}}
	sig, _ := p.Signature()
	payload, _ := json.MarshalIndent(p, "", "  ")
	os.WriteFile(planPath, payload, 0o644)

	broker, _ := NewFileBroker(dir)
	req := Request{Kind: KindPlan, PlanPath: "plan/current.json", PlanSig: sig}
	id, _ := broker.Submit(context.Background(), req)

	// Modify plan to change signature.
	p.Steps = []string{"step", "step2"}
	payload, _ = json.MarshalIndent(p, "", "  ")
	os.WriteFile(planPath, payload, 0o644)

	dec := Decision{ID: id, Status: StatusApproved, ApprovedBy: "tester", Timestamp: time.Now().UTC()}
	bytes, _ := json.MarshalIndent(dec, "", "  ")
	os.WriteFile(filepath.Join(dir, "approvals", "decisions", id+".json"), bytes, 0o644)

	if _, _, err := broker.GetDecision(context.Background(), id); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("expected ErrStalePlan, got %v", err)
	}
}

func TestFileBrokerExpiry(t *testing.T) {
	dir := t.TempDir()
	broker, _ := NewFileBroker(dir, WithPollInterval(50*time.Millisecond))
	req := Request{Kind: KindTool, ToolName: "sandbox_exec", ExpiresAt: time.Now().UTC().Add(100 * time.Millisecond)}
	id, _ := broker.Submit(context.Background(), req)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dec, err := broker.AwaitDecision(ctx, id)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
	if dec.Status != StatusExpired {
		t.Fatalf("expected expired decision, got %s", dec.Status)
	}
}

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleRecord() Record {
	now := time.Now().UTC().UnixMilli()
	return Record{
		Version:    VersionV1,
		Phase:      "plan",
		PlanSig:    "abcd1234",
		LastStepID: "plan-0001",
		Budgets: Budgets{
			MaxWallClockMS:          120000,
			MaxSteps:                10,
			MaxConsecutiveToolSteps: 3,
		},
		Usage: Usage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
			CostUSD:      0.01,
		},
		StartedAt: now,
		UpdatedAt: now,
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	rec := sampleRecord()
	if err := Write(path, rec); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if loaded != rec {
		t.Fatalf("round trip mismatch: %#v vs %#v", loaded, rec)
	}
}

func TestValidateFailures(t *testing.T) {
	rec := sampleRecord()
	rec.Version = "agent.state.v0"
	if err := rec.Validate(); err == nil {
		t.Fatalf("expected version validation error")
	}

	rec = sampleRecord()
	rec.Phase = ""
	if err := rec.Validate(); err == nil {
		t.Fatalf("expected phase validation error")
	}

	rec = sampleRecord()
	rec.StartedAt = 0
	if err := rec.Validate(); err == nil {
		t.Fatalf("expected started_at validation error")
	}
}

func TestWriteCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")
	if err := Write(path, sampleRecord()); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file written: %v", err)
	}
}

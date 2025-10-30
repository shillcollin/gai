package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// VersionV1 is the current on-disk schema identifier.
	VersionV1 = "agent.state.v1"
)

// Budgets captures configured budget limits in the persisted state.
type Budgets struct {
	MaxWallClockMS          int64   `json:"max_wall_clock_ms"`
	MaxSteps                int     `json:"max_steps"`
	MaxConsecutiveToolSteps int     `json:"max_consecutive_tool_steps"`
	MaxTokens               int     `json:"max_tokens"`
	MaxCostUSD              float64 `json:"max_cost_usd"`
}

// Usage represents cumulative resource usage for the task.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Record is the persisted state blob stored at progress/state.json.
type Record struct {
	Version         string   `json:"version"`
	LoopName        string   `json:"loop_name,omitempty"`
	LoopVersion     string   `json:"loop_version,omitempty"`
	Phase           string   `json:"phase"`
	CompletedPhases []string `json:"completed_phases,omitempty"`
	PlanSig         string   `json:"plan_sig,omitempty"`
	LastStepID      string   `json:"last_step_id,omitempty"`
	Budgets         Budgets  `json:"budgets"`
	Usage           Usage    `json:"usage"`
	StartedAt       int64    `json:"started_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

// Validate returns an error if mandatory fields are missing.
func (r *Record) Validate() error {
	if r.Version == "" {
		r.Version = VersionV1
	}
	if r.Version != VersionV1 {
		return fmt.Errorf("state: unsupported version %q", r.Version)
	}
	if r.Phase == "" {
		return errors.New("state: phase is required")
	}
	if r.StartedAt <= 0 {
		return errors.New("state: started_at must be positive")
	}
	if r.UpdatedAt <= 0 {
		return errors.New("state: updated_at must be positive")
	}
	return nil
}

// Write atomically persists the record to path.
func Write(path string, record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state: create dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("state: create tmp: %w", err)
	}
	tmpPath := tmp.Name()

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("state: encode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("state: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("state: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("state: rename: %w", err)
	}
	return nil
}

// Read loads the record from disk.
func Read(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("state: read: %w", err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("state: decode: %w", err)
	}
	if err := rec.Validate(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

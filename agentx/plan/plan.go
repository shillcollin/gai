package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// VersionV1 identifies the plan schema version supported by the agent.
	VersionV1 = "plan.v1"
)

// PlanV1 is the canonical plan structure persisted to progress/plan/current.json.
type PlanV1 struct {
	Version      string   `json:"version"`
	Goal         string   `json:"goal"`
	Steps        []string `json:"steps"`
	AllowedTools []string `json:"allowed_tools"`
	Acceptance   []string `json:"acceptance"`
}

// Validate ensures required fields are present and normalises defaults.
func (p *PlanV1) Validate() error {
	if p.Version == "" {
		p.Version = VersionV1
	}
	if p.Version != VersionV1 {
		return fmt.Errorf("plan: unsupported version %q", p.Version)
	}
	if strings.TrimSpace(p.Goal) == "" {
		return errors.New("plan: goal is required")
	}
	if len(p.Steps) == 0 {
		return errors.New("plan: at least one step is required")
	}
	for i, step := range p.Steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("plan: step %d is empty", i)
		}
	}
	if len(p.AllowedTools) == 0 {
		return errors.New("plan: allowed_tools cannot be empty")
	}
	seen := make(map[string]struct{}, len(p.AllowedTools))
	for i, tool := range p.AllowedTools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			return fmt.Errorf("plan: allowed_tools[%d] is empty", i)
		}
		if _, exists := seen[tool]; exists {
			return fmt.Errorf("plan: allowed_tools[%d] duplicates %q", i, tool)
		}
		seen[tool] = struct{}{}
		p.AllowedTools[i] = tool
	}
	for i, acc := range p.Acceptance {
		if strings.TrimSpace(acc) == "" {
			return fmt.Errorf("plan: acceptance[%d] is empty", i)
		}
	}
	return nil
}

// Signature returns a deterministic sha256 hash of the plan using RFC 8785 canonical JSON.
func (p PlanV1) Signature() (string, error) {
	cp := p
	if err := cp.Validate(); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(cp)
	if err != nil {
		return "", fmt.Errorf("plan: canonical marshal: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// AllowsTool reports whether the tool name appears in AllowedTools.
func (p PlanV1) AllowsTool(name string) bool {
	for _, tool := range p.AllowedTools {
		if tool == name {
			return true
		}
	}
	return false
}

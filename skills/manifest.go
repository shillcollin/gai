package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/shillcollin/gai/sandbox"
	"gopkg.in/yaml.v3"
)

// Manifest represents a skill definition sourced from disk.
type Manifest struct {
	Name         string            `json:"name" yaml:"name"`
	Version      string            `json:"version" yaml:"version"`
	Summary      string            `json:"summary" yaml:"summary"`
	Description  string            `json:"description,omitempty" yaml:"description,omitempty"`
	Tags         []string          `json:"tags,omitempty" yaml:"tags,omitempty"`
	Instructions string            `json:"instructions" yaml:"instructions"`
	Metadata     map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Tools    []ToolBinding    `json:"tools,omitempty" yaml:"tools,omitempty"`
	Sandbox  SandboxConfig    `json:"sandbox" yaml:"sandbox"`
	Evals    []EvaluationSpec `json:"evaluations,omitempty" yaml:"evaluations,omitempty"`
	Created  string           `json:"created,omitempty" yaml:"created,omitempty"`
	Modified string           `json:"modified,omitempty" yaml:"modified,omitempty"`
	baseDir  string           `json:"-" yaml:"-"`
}

// ToolBinding references a tool the skill enables.
type ToolBinding struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Optional    bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// SandboxConfig configures the sandbox session for the skill.
type SandboxConfig struct {
	Session      sandbox.SessionSpec `json:"session" yaml:"session"`
	Warm         bool                `json:"warm,omitempty" yaml:"warm,omitempty"`
	Prewarm      []ExecSpec          `json:"prewarm,omitempty" yaml:"prewarm,omitempty"`
	PostRun      []ExecSpec          `json:"post_run,omitempty" yaml:"post_run,omitempty"`
	WorkspaceDir string              `json:"workspace_dir,omitempty" yaml:"workspace_dir,omitempty"`
	Mounts       []LocalMount        `json:"mounts,omitempty" yaml:"mounts,omitempty"`
	PolicyNote   string              `json:"policy_note,omitempty" yaml:"policy_note,omitempty"`
}

// LocalMount specifies a host directory to mount inside the sandbox.
type LocalMount struct {
	Source   string `json:"source" yaml:"source"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readonly,omitempty" yaml:"readonly,omitempty"`
}

// ExecSpec describes a command executed during prewarming or evaluation.
type ExecSpec struct {
	Name        string        `json:"name,omitempty" yaml:"name,omitempty"`
	Command     []string      `json:"command" yaml:"command"`
	Workdir     string        `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
}

// EvaluationSpec describes a validation executed inside the sandbox.
type EvaluationSpec struct {
	Name          string        `json:"name" yaml:"name"`
	Description   string        `json:"description,omitempty" yaml:"description,omitempty"`
	Command       []string      `json:"command,omitempty" yaml:"command,omitempty"`
	ExpectExit    int           `json:"expect_exit,omitempty" yaml:"expect_exit,omitempty"`
	Timeout       time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	AssertMessage string        `json:"assert_message,omitempty" yaml:"assert_message,omitempty"`
}

// LoadManifest reads a manifest from disk.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: read manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("skills: decode manifest: %w", err)
	}
	manifest.baseDir = filepath.Dir(path)
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if manifest.Metadata == nil {
		manifest.Metadata = map[string]string{}
	}
	if manifest.Metadata["source_path"] == "" {
		manifest.Metadata["source_path"] = filepath.Clean(path)
	}
	return &manifest, nil
}

// Validate ensures the manifest is structurally sound.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("skill name is required")
	}
	if _, err := semver.NewVersion(m.Version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}
	if strings.TrimSpace(m.Summary) == "" {
		return fmt.Errorf("skill summary is required")
	}
	if strings.TrimSpace(m.Instructions) == "" {
		return fmt.Errorf("skill instructions are required")
	}
	if err := m.Sandbox.Session.Validate(); err != nil {
		return fmt.Errorf("sandbox.session: %w", err)
	}
	nameSeen := map[string]struct{}{}
	for i, tool := range m.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("tool %d: name is required", i)
		}
		if _, ok := nameSeen[tool.Name]; ok {
			return fmt.Errorf("tool %q defined multiple times", tool.Name)
		}
		nameSeen[tool.Name] = struct{}{}
	}
	for i, exec := range m.Sandbox.Prewarm {
		if len(exec.Command) == 0 {
			return fmt.Errorf("sandbox.prewarm[%d]: command required", i)
		}
	}
	for i, exec := range m.Sandbox.PostRun {
		if len(exec.Command) == 0 {
			return fmt.Errorf("sandbox.post_run[%d]: command required", i)
		}
	}

	if m.Sandbox.WorkspaceDir != "" {
		if filepath.IsAbs(m.Sandbox.WorkspaceDir) {
			return fmt.Errorf("sandbox.workspace_dir must be relative")
		}
	}
	for i, mount := range m.Sandbox.Mounts {
		if strings.TrimSpace(mount.Source) == "" {
			return fmt.Errorf("sandbox.mounts[%d]: source required", i)
		}
		if filepath.IsAbs(mount.Source) {
			return fmt.Errorf("sandbox.mounts[%d]: source must be relative", i)
		}
		if strings.TrimSpace(mount.Target) == "" {
			return fmt.Errorf("sandbox.mounts[%d]: target required", i)
		}
		if !filepath.IsAbs(mount.Target) {
			return fmt.Errorf("sandbox.mounts[%d]: target must be absolute", i)
		}
	}
	for i, ev := range m.Evals {
		if strings.TrimSpace(ev.Name) == "" {
			return fmt.Errorf("evaluation %d: name is required", i)
		}
		if len(ev.Command) == 0 {
			return fmt.Errorf("evaluation %q: command is required", ev.Name)
		}
	}
	return nil
}

// BaseDir returns the directory containing the manifest file.
func (m Manifest) BaseDir() string {
	return m.baseDir
}

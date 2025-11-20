package sandbox

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Backend identifies a sandbox runtime backend implementation.
type Backend string

const (
	// BackendLocal executes workloads directly on the host OS.
	BackendLocal Backend = "local"
)

// RuntimeSpec describes how the sandbox runtime should be initialised.
type RuntimeSpec struct {
	Backend    Backend           `json:"backend" yaml:"backend"`
	Workdir    string            `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Env        map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Shell      string            `json:"shell,omitempty" yaml:"shell,omitempty"`
}

// Validate ensures the runtime specification is well formed.
func (r RuntimeSpec) Validate() error {
	switch r.Backend {
	case "", BackendLocal:
		// Allow empty so manager defaults to BackendLocal.
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, r.Backend)
	}

	if r.Workdir != "" && !filepath.IsAbs(r.Workdir) {
		return fmt.Errorf("runtime workdir must be absolute: %s", r.Workdir)
	}
	if r.Shell != "" && !filepath.IsAbs(r.Shell) {
		// permit /bin/sh style paths; for bare shell names, ensure no path separators (e.g., "bash").
		if strings.ContainsRune(r.Shell, '/') == false {
			// bare shell name is acceptable
		} else {
			return fmt.Errorf("runtime shell path must be absolute: %s", r.Shell)
		}
	}
	return nil
}

// ResourceLimits captures soft execution limits enforced per command.
type ResourceLimits struct {
	Timeout       time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	CPUSeconds    int           `json:"cpu_seconds,omitempty" yaml:"cpu_seconds,omitempty"`
	MemoryBytes   int64         `json:"memory_bytes,omitempty" yaml:"memory_bytes,omitempty"`
	DiskBytes     int64         `json:"disk_bytes,omitempty" yaml:"disk_bytes,omitempty"`
	NetworkAccess *bool         `json:"network_access,omitempty" yaml:"network_access,omitempty"`
}

// Merge applies overrides from child onto parent.
func (r ResourceLimits) Merge(override ResourceLimits) ResourceLimits {
	out := r
	if r.NetworkAccess != nil {
		val := *r.NetworkAccess
		out.NetworkAccess = &val
	}
	if override.Timeout != 0 {
		out.Timeout = override.Timeout
	}
	if override.CPUSeconds != 0 {
		out.CPUSeconds = override.CPUSeconds
	}
	if override.MemoryBytes != 0 {
		out.MemoryBytes = override.MemoryBytes
	}
	if override.DiskBytes != 0 {
		out.DiskBytes = override.DiskBytes
	}
	if override.NetworkAccess != nil {
		out.NetworkAccess = boolPtr(*override.NetworkAccess)
	}
	return out
}

// Clone returns a deep copy of the resource limits.
func (r ResourceLimits) Clone() ResourceLimits {
	out := r
	if r.NetworkAccess != nil {
		v := *r.NetworkAccess
		out.NetworkAccess = &v
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

// FileMount describes a host directory made available inside the sandbox.
type FileMount struct {
	Source   string `json:"source" yaml:"source"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readonly,omitempty" yaml:"readonly,omitempty"`
}

// Validate ensures the mount is well formed.
func (m FileMount) Validate() error {
	if m.Source == "" {
		return fmt.Errorf("mount source is required")
	}
	if !filepath.IsAbs(m.Target) {
		return fmt.Errorf("mount target must be absolute: %s", m.Target)
	}
	return nil
}

// FileTemplate adds a synthetic file into the sandbox filesystem.
type FileTemplate struct {
    Path     string      `json:"path" yaml:"path"`
    Contents string      `json:"contents" yaml:"contents"`
    Mode     fs.FileMode `json:"mode,omitempty" yaml:"mode,omitempty"`
    Owner    string      `json:"owner,omitempty" yaml:"owner,omitempty"`
}

// Validate ensures template metadata is valid.
func (t FileTemplate) Validate() error {
	if t.Path == "" {
		return fmt.Errorf("template path is required")
	}
	if !filepath.IsAbs(t.Path) {
		return fmt.Errorf("template path must be absolute: %s", t.Path)
	}
	return nil
}

// FilesystemSpec captures mounts and synthetic files for the sandbox.
type FilesystemSpec struct {
	Mounts    []FileMount    `json:"mounts,omitempty" yaml:"mounts,omitempty"`
	Templates []FileTemplate `json:"templates,omitempty" yaml:"templates,omitempty"`
}

// Validate ensures filesystem description is well formed.
func (f FilesystemSpec) Validate() error {
	for i, m := range f.Mounts {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("mount %d: %w", i, err)
		}
	}
	for i, t := range f.Templates {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("template %d: %w", i, err)
		}
	}
	return nil
}

// SessionSpec ties together runtime, filesystem and limits.
type SessionSpec struct {
	Name       string            `json:"name,omitempty" yaml:"name,omitempty"`
	Runtime    RuntimeSpec       `json:"runtime" yaml:"runtime"`
	Filesystem FilesystemSpec    `json:"filesystem,omitempty" yaml:"filesystem,omitempty"`
	Limits     ResourceLimits    `json:"limits,omitempty" yaml:"limits,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Warm       bool              `json:"warm,omitempty" yaml:"warm,omitempty"`
}

// Validate ensures the session spec is correct.
func (s SessionSpec) Validate() error {
	if err := s.Runtime.Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	if err := s.Filesystem.Validate(); err != nil {
		return fmt.Errorf("filesystem: %w", err)
	}
	return nil
}

// ExecOptions parameterises sandbox command execution.
type ExecOptions struct {
	Command []string
	Env     map[string]string
	Workdir string
	Stdin   []byte
	Stdout  io.Writer
	Stderr  io.Writer

	Timeout   time.Duration
	Mounts    []FileMount
	Templates []FileTemplate
}

// ExecResult captures execution metadata.
type ExecResult struct {
    Command       []string
    ExitCode      int
    Duration      time.Duration
    Stdout        string
    Stderr        string
    AppliedLimits ResourceLimits
}

// SessionAssets describe host resources staged into a sandbox session.
type SessionAssets struct {
    Workspace string
    Mounts    []FileMount
}

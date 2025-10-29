package skills

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/shillcollin/gai/sandbox"
)

// Skill represents a validated and fingerprinted skill.
type Skill struct {
	Manifest    *Manifest
	Fingerprint string
}

// New constructs a Skill from a validated manifest.
func New(manifest *Manifest) (*Skill, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is nil")
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	fp := manifestFingerprint(*manifest)
	return &Skill{
		Manifest:    manifest,
		Fingerprint: fp,
	}, nil
}

// SessionSpec returns the sandbox session specification for the skill.
func (s *Skill) SessionSpec() sandbox.SessionSpec {
	return s.Manifest.Sandbox.Session
}

// Tools returns the list of tool names associated with the skill.
func (s *Skill) Tools() []string {
    out := make([]string, 0, len(s.Manifest.Tools))
    for _, tool := range s.Manifest.Tools {
        out = append(out, tool.Name)
    }
    return out
}

// WorkspacePath returns the absolute workspace directory path for the skill, if configured.
func (s *Skill) WorkspacePath() string {
    if s == nil || s.Manifest == nil {
        return ""
    }
    dir := strings.TrimSpace(s.Manifest.Sandbox.WorkspaceDir)
    if dir == "" {
        return ""
    }
    return filepath.Join(s.Manifest.BaseDir(), dir)
}

// ResolvedMount describes a local directory mounted into the sandbox.
type ResolvedMount struct {
    Source   string
    Target   string
    ReadOnly bool
}

// LocalMounts returns the skill's mounts with absolute host paths.
func (s *Skill) LocalMounts() []ResolvedMount {
    if s == nil || s.Manifest == nil {
        return nil
    }
    mounts := s.Manifest.Sandbox.Mounts
    if len(mounts) == 0 {
        return nil
    }
    base := s.Manifest.BaseDir()
    out := make([]ResolvedMount, 0, len(mounts))
    for _, mount := range mounts {
        out = append(out, ResolvedMount{
            Source:   filepath.Join(base, mount.Source),
            Target:   mount.Target,
            ReadOnly: mount.ReadOnly,
        })
    }
    return out
}

// Prewarm executes configured prewarm commands within the provided session.
func (s *Skill) Prewarm(ctx context.Context, session *sandbox.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	for _, step := range s.Manifest.Sandbox.Prewarm {
		opts := sandbox.ExecOptions{
			Command: step.Command,
			Workdir: step.Workdir,
			Timeout: step.Timeout,
		}
		res, err := session.Exec(ctx, opts)
		if err != nil {
			return fmt.Errorf("prewarm %q failed: %w", step.Name, err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("prewarm %q exit code %d", step.Name, res.ExitCode)
		}
	}
	return nil
}

// RunEvaluations executes all evaluation specs inside a fresh session managed by manager.
func (s *Skill) RunEvaluations(ctx context.Context, manager *sandbox.Manager) ([]EvaluationResult, error) {
	if manager == nil {
		return nil, fmt.Errorf("sandbox manager required")
	}
	session, err := manager.CreateSession(ctx, s.Manifest.Sandbox.Session)
	if err != nil {
		return nil, err
	}
	defer manager.Destroy(session.ID)

	if s.Manifest.Sandbox.Warm {
		if err := s.Prewarm(ctx, session); err != nil {
			return nil, err
		}
	}

	results := make([]EvaluationResult, 0, len(s.Manifest.Evals))
	for _, spec := range s.Manifest.Evals {
		start := time.Now()
		res, err := session.Exec(ctx, sandbox.ExecOptions{
			Command: spec.Command,
			Timeout: spec.Timeout,
		})
		duration := time.Since(start)
		result := EvaluationResult{
			Spec:     spec,
			ExitCode: res.ExitCode,
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			Duration: duration,
		}
		if err != nil {
			result.Error = err
		} else if spec.ExpectExit != 0 && res.ExitCode != spec.ExpectExit {
			result.Error = fmt.Errorf("expected exit %d, got %d", spec.ExpectExit, res.ExitCode)
		} else if spec.ExpectExit == 0 && res.ExitCode != 0 {
			result.Error = fmt.Errorf("expected exit 0, got %d", res.ExitCode)
		}
		results = append(results, result)
	}
	return results, nil
}

// EvaluationResult captures the outcome of a single evaluation.
type EvaluationResult struct {
	Spec     EvaluationSpec
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	Error    error
}

func manifestFingerprint(m Manifest) string {
    hash := sha256.New()
    hash.Write([]byte(m.Name))
    hash.Write([]byte(m.Version))
    hash.Write([]byte(m.Summary))
	hash.Write([]byte(m.Instructions))
	hash.Write([]byte(m.Description))
	tags := append([]string(nil), m.Tags...)
	sort.Strings(tags)
	for _, tag := range tags {
		hash.Write([]byte(tag))
	}
	tools := append([]ToolBinding(nil), m.Tools...)
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	for _, tool := range tools {
		hash.Write([]byte(tool.Name))
		hash.Write([]byte(tool.Description))
	}
	hash.Write([]byte(m.Sandbox.Session.Runtime.Image))
	hash.Write([]byte(fmt.Sprintf("%v", m.Sandbox.Session.Runtime)))
    for _, mount := range m.Sandbox.Session.Filesystem.Mounts {
        hash.Write([]byte(mount.Source))
        hash.Write([]byte(mount.Target))
    }
    for _, tmpl := range m.Sandbox.Session.Filesystem.Templates {
        hash.Write([]byte(tmpl.Path))
        hash.Write([]byte(tmpl.Contents))
    }
    hash.Write([]byte(m.Sandbox.WorkspaceDir))
    for _, mount := range m.Sandbox.Mounts {
        hash.Write([]byte(mount.Source))
        hash.Write([]byte(mount.Target))
        if mount.ReadOnly {
            hash.Write([]byte("ro"))
        }
    }
    for _, step := range m.Sandbox.Prewarm {
        hash.Write([]byte(strings.Join(step.Command, "\x00")))
    }
    for _, step := range m.Sandbox.PostRun {
        hash.Write([]byte(strings.Join(step.Command, "\x00")))
    }
    return hex.EncodeToString(hash.Sum(nil))
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

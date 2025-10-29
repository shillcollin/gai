package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shillcollin/gai/sandbox"
	"github.com/shillcollin/gai/skills"
	"gopkg.in/yaml.v3"
)

// Manifest describes bundle-level configuration shared across skills.
type Manifest struct {
	Name                string   `yaml:"name" json:"name"`
	Version             string   `yaml:"version" json:"version"`
	DefaultSkill        string   `yaml:"default_skill" json:"default_skill"`
	MaxSteps            int      `yaml:"max_steps" json:"max_steps"`
	ProviderPreferences []string `yaml:"provider_preferences" json:"provider_preferences"`
	SharedWorkspace     string   `yaml:"shared_workspace" json:"shared_workspace"`
}

// Bundle captures an agent bundle directory with multiple skills.
type Bundle struct {
	Path                string
	Manifest            Manifest
	Skills              map[string]*skills.Skill
	skillDirs           map[string]string
	sharedWorkspacePath string
	configs             map[string]SkillConfig
}

// SkillConfig captures filesystem assets resolved for a skill.
type SkillConfig struct {
	Skill     *skills.Skill
	Workspace string
	Mounts    []sandbox.FileMount
}

// LoadBundle parses the agent bundle rooted at dir.
func LoadBundle(dir string) (*Bundle, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("agent bundle path is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("agent: resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("agent: stat bundle: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("agent: %s is not a directory", abs)
	}

	bundle := &Bundle{
		Path:      abs,
		Skills:    map[string]*skills.Skill{},
		skillDirs: map[string]string{},
		configs:   map[string]SkillConfig{},
	}

	if err := bundle.loadManifest(); err != nil {
		return nil, err
	}
	if err := bundle.loadSkills(); err != nil {
		return nil, err
	}
	if bundle.Manifest.DefaultSkill == "" {
		// Select first skill alphabetically as default.
		names := make([]string, 0, len(bundle.Skills))
		for name := range bundle.Skills {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			bundle.Manifest.DefaultSkill = names[0]
		}
	}
	if bundle.Manifest.DefaultSkill != "" {
		if _, ok := bundle.Skills[bundle.Manifest.DefaultSkill]; !ok {
			return nil, fmt.Errorf("agent: default_skill %q not found", bundle.Manifest.DefaultSkill)
		}
	}
	if bundle.Manifest.MaxSteps <= 0 {
		bundle.Manifest.MaxSteps = 6
	}

	return bundle, nil
}

func (b *Bundle) loadManifest() error {
	manifestPath := filepath.Join(b.Path, "agent.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// default manifest
			b.Manifest = Manifest{
				Name:    filepath.Base(b.Path),
				Version: "0.0.0",
			}
			return nil
		}
		return fmt.Errorf("agent: read agent.yaml: %w", err)
	}
	if err := yaml.Unmarshal(data, &b.Manifest); err != nil {
		return fmt.Errorf("agent: decode agent.yaml: %w", err)
	}
	if strings.TrimSpace(b.Manifest.Name) == "" {
		b.Manifest.Name = filepath.Base(b.Path)
	}
	if strings.TrimSpace(b.Manifest.Version) == "" {
		b.Manifest.Version = "0.0.0"
	}
	if strings.TrimSpace(b.Manifest.SharedWorkspace) != "" {
		if filepath.IsAbs(b.Manifest.SharedWorkspace) {
			return fmt.Errorf("agent: shared_workspace must be relative")
		}
		abs := filepath.Join(b.Path, b.Manifest.SharedWorkspace)
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("agent: shared_workspace: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("agent: shared_workspace %s is not a directory", abs)
		}
		b.sharedWorkspacePath = abs
		// normalize stored value
		b.Manifest.SharedWorkspace = filepath.Clean(b.Manifest.SharedWorkspace)
	}
	return nil
}

func (b *Bundle) loadSkills() error {
	skillsRoot := filepath.Join(b.Path, "SKILLS")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return fmt.Errorf("agent: read skills directory: %w", err)
	}
	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(skillsRoot, entry.Name())
		manifestPath := filepath.Join(dir, "skill.yaml")
		if _, err := os.Stat(manifestPath); err != nil {
			// Try .yml fallback
			alt := filepath.Join(dir, "skill.yml")
			if _, err2 := os.Stat(alt); err2 != nil {
				continue
			}
			manifestPath = alt
		}
		manifest, err := skills.LoadManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("agent: load %s: %w", manifestPath, err)
		}
		skill, err := skills.New(manifest)
		if err != nil {
			return fmt.Errorf("agent: build skill %s: %w", manifestPath, err)
		}
		name := manifest.Name
		if strings.TrimSpace(name) == "" {
			name = entry.Name()
		}
		if _, exists := b.Skills[name]; exists {
			return fmt.Errorf("agent: duplicate skill name %q", name)
		}
		cfg, err := b.buildSkillConfig(skill)
		if err != nil {
			return fmt.Errorf("agent: skill %s: %w", name, err)
		}
		b.Skills[name] = skill
		b.skillDirs[name] = dir
		b.configs[name] = cfg
		found = true
	}
	if !found {
		return fmt.Errorf("agent: no skills found under %s", skillsRoot)
	}
	return nil
}

func (b *Bundle) buildSkillConfig(skill *skills.Skill) (SkillConfig, error) {
	if skill == nil {
		return SkillConfig{}, fmt.Errorf("nil skill")
	}
	cfg := SkillConfig{Skill: skill}
	workspace := strings.TrimSpace(skill.WorkspacePath())
	if workspace != "" {
		info, err := os.Stat(workspace)
		if err != nil {
			return cfg, fmt.Errorf("workspace: %w", err)
		}
		if !info.IsDir() {
			return cfg, fmt.Errorf("workspace %s is not a directory", workspace)
		}
		cfg.Workspace = workspace
	}
	mounts := skill.LocalMounts()
	for _, mount := range mounts {
		info, err := os.Stat(mount.Source)
		if err != nil {
			return cfg, fmt.Errorf("mount %s: %w", mount.Source, err)
		}
		if !info.IsDir() {
			return cfg, fmt.Errorf("mount source %s must be a directory", mount.Source)
		}
		cfg.Mounts = append(cfg.Mounts, sandbox.FileMount{
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	if b.sharedWorkspacePath != "" {
		cfg.Mounts = append(cfg.Mounts, sandbox.FileMount{
			Source:   b.sharedWorkspacePath,
			Target:   "/workspace/shared",
			ReadOnly: true,
		})
	}
	return cfg, nil
}

// Skill returns the named skill from the bundle.
func (b *Bundle) Skill(name string) (*skills.Skill, bool) {
    sk, ok := b.Skills[name]
    return sk, ok
}

// SkillConfig returns the resolved config for a skill.
func (b *Bundle) SkillConfig(name string) (SkillConfig, bool) {
    cfg, ok := b.configs[name]
    return cfg, ok
}

// DefaultSkill returns the configured default skill.
func (b *Bundle) DefaultSkill() (*skills.Skill, bool) {
    if b.Manifest.DefaultSkill == "" {
        return nil, false
    }
	return b.Skill(b.Manifest.DefaultSkill)
}

// SharedWorkspacePath returns the absolute path to the shared workspace directory, if configured.
func (b *Bundle) SharedWorkspacePath() string {
	return b.sharedWorkspacePath
}

// SkillDir returns the absolute directory for the named skill bundle.
func (b *Bundle) SkillDir(name string) (string, bool) {
    dir, ok := b.skillDirs[name]
    return dir, ok
}

// SkillNames returns the list of skill identifiers in the bundle.
func (b *Bundle) SkillNames() []string {
    names := make([]string, 0, len(b.Skills))
    for name := range b.Skills {
        names = append(names, name)
    }
    sort.Strings(names)
    return names
}

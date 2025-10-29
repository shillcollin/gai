package skills

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
)

// Registry maintains a catalog of skills keyed by name and version.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*Skill),
	}
}

// Register inserts a skill into the registry.
func (r *Registry) Register(skill *Skill) error {
	if skill == nil || skill.Manifest == nil {
		return fmt.Errorf("skill is nil")
	}
	key := registryKey(skill.Manifest.Name, skill.Manifest.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[key]; exists {
		return fmt.Errorf("skill %s already registered", key)
	}
	r.skills[key] = skill
	return nil
}

// Get fetches a skill by name/version.
func (r *Registry) Get(name, version string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[registryKey(name, version)]
	return s, ok
}

// Latest returns the highest version registered for a skill name.
func (r *Registry) Latest(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *Skill
	for key, skill := range r.skills {
		if !strings.HasPrefix(key, strings.ToLower(name)+"@") {
			continue
		}
		if latest == nil || compareVersions(skill.Manifest.Version, latest.Manifest.Version) > 0 {
			latest = skill
		}
	}
	if latest == nil {
		return nil, false
	}
	return latest, true
}

// LoadDir walks a directory and registers all manifest files.
func (r *Registry) LoadDir(paths ...string) error {
	for _, path := range paths {
		manifest, err := LoadManifest(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		skill, err := New(manifest)
		if err != nil {
			return fmt.Errorf("build skill %s: %w", manifest.Name, err)
		}
		if err := r.Register(skill); err != nil {
			return err
		}
	}
	return nil
}

func registryKey(name, version string) string {
	return strings.ToLower(name) + "@" + version
}

func compareVersions(a, b string) int {
	av, err1 := semverParse(a)
	bv, err2 := semverParse(b)
	if err1 != nil || err2 != nil {
		return strings.Compare(a, b)
	}
	return av.Compare(bv)
}

func semverParse(v string) (*semver.Version, error) {
	return semver.NewVersion(v)
}

// DefaultManifestGlob returns standard manifest glob pattern.
func DefaultManifestGlob() []string {
	return []string{"skills/*.skill.yaml", "skills/*.skill.yml"}
}

// ExpandGlobs resolves glob patterns to manifest paths.
func ExpandGlobs(globs []string) ([]string, error) {
	var out []string
	for _, pattern := range globs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}
		out = append(out, matches...)
	}
	return out, nil
}

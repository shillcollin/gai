package prompts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
)

type Registry struct {
	fs          fs.FS
	overrideDir string
	helpers     template.FuncMap

	mu      sync.RWMutex
	prompts map[string]map[string]*templateEntry
}

type templateEntry struct {
	tmpl        *template.Template
	fingerprint string
	source      string
}

// PromptID identifies a prompt version at render time.
type PromptID struct {
	Name        string
	Version     string
	Fingerprint string
}

// RegistryOption customises registry behaviour.
type RegistryOption func(*Registry)

// WithOverrideDir enables runtime overrides from a local directory.
func WithOverrideDir(dir string) RegistryOption {
	return func(r *Registry) { r.overrideDir = dir }
}

// WithHelperFunc registers a helper function.
func WithHelperFunc(name string, fn any) RegistryOption {
	return func(r *Registry) {
		if r.helpers == nil {
			r.helpers = template.FuncMap{}
		}
		r.helpers[name] = fn
	}
}

// WithHelpers registers multiple helper functions.
func WithHelpers(funcs template.FuncMap) RegistryOption {
	return func(r *Registry) {
		if r.helpers == nil {
			r.helpers = template.FuncMap{}
		}
		for k, v := range funcs {
			r.helpers[k] = v
		}
	}
}

// NewRegistry constructs a prompt registry.
func NewRegistry(promptFS fs.FS, opts ...RegistryOption) *Registry {
	r := &Registry{fs: promptFS, helpers: template.FuncMap{}}
	for _, opt := range opts {
		opt(r)
	}
	r.prompts = map[string]map[string]*templateEntry{}
	return r
}

// Reload parses templates from the underlying filesystem and override directory.
func (r *Registry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	prompts := map[string]map[string]*templateEntry{}

	if err := fs.WalkDir(r.fs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}
		name, version, err := parseFilename(path)
		if err != nil {
			return fmt.Errorf("parse prompt filename %s: %w", path, err)
		}
		data, err := fs.ReadFile(r.fs, path)
		if err != nil {
			return err
		}
		entry, err := r.parseTemplate(name, version, data)
		if err != nil {
			return err
		}
		entry.source = path
		addTemplate(prompts, name, version, entry)
		return nil
	}); err != nil {
		return err
	}

	if r.overrideDir != "" {
		if err := filepath.WalkDir(r.overrideDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".tmpl") {
				return nil
			}
			name, version, err := parseFilename(d.Name())
			if err != nil {
				return fmt.Errorf("parse override %s: %w", path, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry, err := r.parseTemplate(name, version, data)
			if err != nil {
				return err
			}
			entry.source = path
			addTemplate(prompts, name, version, entry)
			return nil
		}); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	r.prompts = prompts
	return nil
}

func (r *Registry) parseTemplate(name, version string, data []byte) (*templateEntry, error) {
	tmpl, err := template.New(name).Funcs(r.helpers).Parse(string(data))
	if err != nil {
		return nil, err
	}
	sha := sha256.Sum256(data)
	return &templateEntry{tmpl: tmpl, fingerprint: hex.EncodeToString(sha[:])}, nil
}

func addTemplate(store map[string]map[string]*templateEntry, name, version string, entry *templateEntry) {
	versions, ok := store[name]
	if !ok {
		versions = map[string]*templateEntry{}
		store[name] = versions
	}
	versions[version] = entry
}

// Render executes the selected prompt template.
func (r *Registry) Render(ctx context.Context, name, version string, data any) (string, PromptID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.prompts[name]
	if !ok || len(versions) == 0 {
		return "", PromptID{}, fmt.Errorf("prompt %s not found", name)
	}
	if version == "" {
		version = latestVersion(versions)
	}
	entry, ok := versions[version]
	if !ok {
		return "", PromptID{}, fmt.Errorf("prompt %s@%s not found", name, version)
	}

	buf := &bytes.Buffer{}
	if err := entry.tmpl.Execute(buf, data); err != nil {
		return "", PromptID{}, err
	}

	return buf.String(), PromptID{Name: name, Version: version, Fingerprint: entry.fingerprint}, nil
}

// ListVersions returns sorted versions for the prompt.
func (r *Registry) ListVersions(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions := r.prompts[name]
	if len(versions) == 0 {
		return nil
	}
	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// HasOverride returns the override path for a prompt if present.
func (r *Registry) HasOverride(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions := r.prompts[name]
	for _, entry := range versions {
		if r.overrideDir != "" && strings.HasPrefix(entry.source, r.overrideDir) {
			return entry.source
		}
	}
	return ""
}

// GetTemplate returns the underlying template.
func (r *Registry) GetTemplate(name, version string) (*template.Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.prompts[name]
	if len(versions) == 0 {
		return nil, fmt.Errorf("prompt %s not found", name)
	}
	if version == "" {
		version = latestVersion(versions)
	}
	entry, ok := versions[version]
	if !ok {
		return nil, fmt.Errorf("prompt %s@%s not found", name, version)
	}
	return entry.tmpl, nil
}

func latestVersion(versions map[string]*templateEntry) string {
	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	sort.Strings(out)
	return out[len(out)-1]
}

func parseFilename(filename string) (name, version string, err error) {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".tmpl")
	parts := strings.Split(base, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid prompt filename: %s", filename)
	}
	return parts[0], parts[1], nil
}

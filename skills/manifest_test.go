package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shillcollin/gai/sandbox"
)

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.yaml")
	content := `
name: test-skill
version: "1.0.0"
summary: Test skill
instructions: |
  Follow the plan precisely.
sandbox:
  session:
    runtime:
      backend: dagger
      image: alpine:3.19
      workdir: /workspace
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if manifest.Name != "test-skill" {
		t.Fatalf("expected name test-skill, got %s", manifest.Name)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", manifest.Version)
	}
	if manifest.Sandbox.Session.Runtime.Image != "alpine:3.19" {
		t.Fatalf("unexpected runtime image: %s", manifest.Sandbox.Session.Runtime.Image)
	}
}

func TestManifestValidation(t *testing.T) {
	manifest := Manifest{
		Name:         "invalid",
		Version:      "not-semver",
		Summary:      "summary",
		Instructions: "do it",
		Sandbox: SandboxConfig{
			Session: sandbox.SessionSpec{
				Runtime: sandbox.RuntimeSpec{
					Backend: sandbox.BackendDagger,
					Image:   "alpine:3.19",
				},
			},
		},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected validation error for invalid version")
	}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundle(t *testing.T) {
	tdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tdir, "shared"), 0o755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tdir, "agent.yaml"), []byte("name: demo\nversion: 0.0.1\ndefault_skill: python\nshared_workspace: shared\n"), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	skillDir := filepath.Join(tdir, "SKILLS", "python")
	if err := os.MkdirAll(filepath.Join(skillDir, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	skillYAML := `name: python
version: "1.0.0"
summary: demo skill
instructions: |
  Do things.
tools:
  - name: sandbox_exec
sandbox:
  workspace_dir: ./workspace
  session:
    runtime:
      image: alpine:3.19
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(skillYAML), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "workspace", "README.md"), []byte("demo"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	bundle, err := LoadBundle(tdir)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}
	if bundle.Manifest.Name != "demo" {
		t.Fatalf("expected bundle name demo, got %s", bundle.Manifest.Name)
	}
	names := bundle.SkillNames()
	if len(names) != 1 || names[0] != "python" {
		t.Fatalf("unexpected skill names: %v", names)
	}
	cfg, ok := bundle.SkillConfig("python")
	if !ok {
		t.Fatalf("skill config not found")
	}
	if cfg.Workspace == "" {
		t.Fatalf("workspace path not resolved")
	}
	if len(cfg.Mounts) != 1 {
		t.Fatalf("expected shared workspace mount, got %d", len(cfg.Mounts))
	}
}

package prompts

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestRegistryRender(t *testing.T) {
	fs := fstest.MapFS{
		"summary@1.0.0.tmpl": {Data: []byte("Summary: {{.Text}}")},
		"summary@1.1.0.tmpl": {Data: []byte("Summary v1.1: {{.Text}}")},
	}
	reg := NewRegistry(fs)
	if err := reg.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	out, id, err := reg.Render(context.Background(), "summary", "1.1.0", map[string]any{"Text": "hello"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "Summary v1.1: hello" {
		t.Fatalf("unexpected output: %s", out)
	}
	if id.Name != "summary" || id.Version != "1.1.0" || id.Fingerprint == "" {
		t.Fatalf("unexpected prompt id: %+v", id)
	}
}

func TestRegistryLatestVersion(t *testing.T) {
	fs := fstest.MapFS{
		"demo@1.0.0.tmpl": {Data: []byte("v1")},
		"demo@1.2.0.tmpl": {Data: []byte("v1.2")},
	}
	reg := NewRegistry(fs)
	if err := reg.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	out, _, err := reg.Render(context.Background(), "demo", "", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "v1.2" {
		t.Fatalf("expected latest version, got %s", out)
	}
}

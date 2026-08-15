package playbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatDeptPlaybookInject_BaseAndLocal(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, ".orchestra", "playbooks")
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(LocalRelDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "frontend@web.md"), []byte("# L2\nuse pnpm"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(LocalRelDir), "frontend@web.md"), []byte("prefer vitest"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FormatDeptPlaybookInject(root, "frontend@web")
	if !strings.Contains(got, "<dept_playbook") || !strings.Contains(got, "use pnpm") || !strings.Contains(got, "prefer vitest") {
		t.Fatalf("inject = %q", got)
	}
	if !strings.Contains(got, "local overlay") {
		t.Fatalf("missing overlay marker: %q", got)
	}
}

func TestFormatDeptPlaybookInject_ApprovedOverlayMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	ref := "approve vitest overlay"
	log := "## 2026-08-15 · decision\n- A: " + ref + "\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "decisions.md"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	localDir := filepath.Join(root, filepath.FromSlash(LocalRelDir))
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndecision_ref: " + ref + "\n---\n\nprefer vitest\n"
	if err := os.WriteFile(filepath.Join(localDir, "frontend@web.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FormatDeptPlaybookInject(root, "frontend@web")
	if !strings.Contains(got, "approved via decisions.md") {
		t.Fatalf("inject = %q", got)
	}
}

func TestFormatDeptPlaybookInject_InheritsBaseDept(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, ".orchestra", "playbooks")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "frontend.md"), []byte("base rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FormatDeptPlaybookInject(root, "frontend@web")
	if !strings.Contains(got, "base rules") {
		t.Fatalf("inject = %q", got)
	}
}

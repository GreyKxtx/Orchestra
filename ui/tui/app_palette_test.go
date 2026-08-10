package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListWorkspaceFiles_deepPathsIncluded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "deep.go")
	if err := os.MkdirAll(filepath.Dir(deep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte("package deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shallow := filepath.Join(root, "top.go")
	if err := os.WriteFile(shallow, []byte("package top\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := listWorkspaceFiles(root, nil)
	want := "a/b/c/d/e/deep.go"
	foundDeep, foundTop := false, false
	for _, p := range got {
		if p == want {
			foundDeep = true
		}
		if p == "top.go" {
			foundTop = true
		}
	}
	if !foundDeep {
		t.Fatalf("deep file missing from listing (>%d levels); got %v", 4, got)
	}
	if !foundTop {
		t.Fatalf("shallow file missing; got %v", got)
	}
}

func TestListWorkspaceFiles_respectsExcludeDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(root, "vendor", "lib")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "skip.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := listWorkspaceFiles(root, []string{"vendor"})
	for _, p := range got {
		if p == "vendor/lib/skip.go" {
			t.Fatalf("excluded vendor file listed: %v", got)
		}
	}
}

func TestInExcludedTree(t *testing.T) {
	ex := map[string]struct{}{"node_modules": {}}
	if !inExcludedTree("node_modules/pkg/index.js", ex) {
		t.Fatal("expected excluded")
	}
	if inExcludedTree("internal/core/session.go", ex) {
		t.Fatal("expected included")
	}
}

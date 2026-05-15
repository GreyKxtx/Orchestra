package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPreviewRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSPreview_BasicSearchReplace(t *testing.T) {
	r, root := newPreviewRunner(t)

	content := "package main\n\nfunc Hello() string {\n\treturn \"world\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "main.go",
		Search:  "return \"world\"",
		Replace: "return \"earth\"",
	})
	if err != nil {
		t.Fatalf("FSPreview: %v", err)
	}
	if resp.Path != "main.go" {
		t.Errorf("path: got %q, want %q", resp.Path, "main.go")
	}
	if !strings.Contains(resp.Diff, "-\treturn \"world\"") {
		t.Errorf("diff missing removed line, got:\n%s", resp.Diff)
	}
	if !strings.Contains(resp.Diff, "+\treturn \"earth\"") {
		t.Errorf("diff missing added line, got:\n%s", resp.Diff)
	}
	// File on disk must NOT be modified.
	got, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(got) != content {
		t.Errorf("file was modified on disk — should not happen")
	}
}

func TestFSPreview_SearchNotFound_ReturnsError(t *testing.T) {
	r, root := newPreviewRunner(t)

	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "f.go",
		Search:  "nonexistent string xyz",
		Replace: "something",
	})
	if err == nil {
		t.Fatal("expected error when search not found, got nil")
	}
}

func TestFSPreview_EmptyPath_ReturnsError(t *testing.T) {
	r, _ := newPreviewRunner(t)
	_, err := r.FSPreview(context.Background(), FSPreviewRequest{Path: "", Search: "x", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSPreview_EmptySearch_ReturnsError(t *testing.T) {
	r, root := newPreviewRunner(t)
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSPreview(context.Background(), FSPreviewRequest{Path: "f.go", Search: "", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty search")
	}
}

func TestFSPreview_DryRunReadsFromStaging(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	// Stage content without writing to disk.
	r.stageFile("staged.go", "package main\n\nfunc Foo() {}\n", "fakehash")

	resp, err := r.FSPreview(context.Background(), FSPreviewRequest{
		Path:    "staged.go",
		Search:  "func Foo() {}",
		Replace: "func Bar() {}",
	})
	if err != nil {
		t.Fatalf("FSPreview from staging: %v", err)
	}
	if !strings.Contains(resp.Diff, "-func Foo()") {
		t.Errorf("expected staged content in diff, got:\n%s", resp.Diff)
	}
}

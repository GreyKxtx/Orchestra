package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/store"
)

func newEditRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func readTestFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func TestFSEdit_BasicReplace(t *testing.T) {
	r, root := newEditRunner(t)
	writeTestFile(t, root, "a.go", "package main\n\nfunc hello() {}\n")

	resp, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:    "a.go",
		Search:  "func hello() {}",
		Replace: "func hello() { println(\"hi\") }",
	})
	if err != nil {
		t.Fatalf("FSEdit: %v", err)
	}
	if resp.Path != "a.go" {
		t.Errorf("unexpected path: %s", resp.Path)
	}
	if resp.FileHash == "" {
		t.Error("expected non-empty file_hash")
	}

	got := readTestFile(t, root, "a.go")
	if got != "package main\n\nfunc hello() { println(\"hi\") }\n" {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestFSEdit_WithFileHash(t *testing.T) {
	r, root := newEditRunner(t)
	content := "package main\n\nconst x = 1\n"
	writeTestFile(t, root, "b.go", content)
	fileHash := store.ComputeSHA256([]byte(content))

	_, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:     "b.go",
		Search:   "const x = 1",
		Replace:  "const x = 42",
		FileHash: fileHash,
	})
	if err != nil {
		t.Fatalf("FSEdit with hash: %v", err)
	}

	got := readTestFile(t, root, "b.go")
	if got != "package main\n\nconst x = 42\n" {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestFSEdit_SearchNotFound(t *testing.T) {
	r, root := newEditRunner(t)
	writeTestFile(t, root, "c.go", "package main\n")

	_, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:    "c.go",
		Search:  "this string does not exist",
		Replace: "replaced",
	})
	if err == nil {
		t.Fatal("expected StaleContent error for missing search string")
	}
}

func TestFSEdit_AmbiguousMatch(t *testing.T) {
	r, root := newEditRunner(t)
	writeTestFile(t, root, "d.go", "foo\nfoo\n")

	_, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:    "d.go",
		Search:  "foo",
		Replace: "bar",
	})
	if err == nil {
		t.Fatal("expected AmbiguousMatch error for duplicate search string")
	}
}

func TestFSEdit_EmptyPath(t *testing.T) {
	r, _ := newEditRunner(t)
	_, err := r.FSEdit(context.Background(), FSEditRequest{Search: "x", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSEdit_EmptySearch(t *testing.T) {
	r, _ := newEditRunner(t)
	_, err := r.FSEdit(context.Background(), FSEditRequest{Path: "x.go", Replace: "y"})
	if err == nil {
		t.Fatal("expected error for empty search")
	}
}

func TestFSEdit_NewHashIsCorrect(t *testing.T) {
	r, root := newEditRunner(t)
	writeTestFile(t, root, "e.go", "hello\n")

	resp, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:    "e.go",
		Search:  "hello",
		Replace: "world",
	})
	if err != nil {
		t.Fatalf("FSEdit: %v", err)
	}

	expected := store.ComputeSHA256([]byte("world\n"))
	if resp.FileHash != expected {
		t.Errorf("FileHash after edit: got %s, want %s", resp.FileHash, expected)
	}
}

func TestFSEdit_DeleteLine(t *testing.T) {
	r, root := newEditRunner(t)
	writeTestFile(t, root, "f.go", "line1\nline2\nline3\n")

	_, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:    "f.go",
		Search:  "line2\n",
		Replace: "",
	})
	if err != nil {
		t.Fatalf("FSEdit delete: %v", err)
	}

	got := readTestFile(t, root, "f.go")
	if got != "line1\nline3\n" {
		t.Errorf("unexpected content after delete: %q", got)
	}
}

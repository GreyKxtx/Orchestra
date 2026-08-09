package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
)

func newWriteRunner(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSWrite_CreateNewFile(t *testing.T) {
	r, root := newWriteRunner(t)

	resp, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "hello.txt",
		Content:      "hello world\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}
	if resp.Path != "hello.txt" {
		t.Errorf("unexpected path: %s", resp.Path)
	}
	if resp.BytesWritten != len("hello world\n") {
		t.Errorf("unexpected bytes_written: %d", resp.BytesWritten)
	}
	want := cache.ComputeSHA256([]byte("hello world\n"))
	if resp.FileHash != want {
		t.Errorf("file_hash mismatch: got %s, want %s", resp.FileHash, want)
	}

	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello world\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestFSWrite_MustNotExist_FailsIfExists(t *testing.T) {
	r, root := newWriteRunner(t)
	_ = os.WriteFile(filepath.Join(root, "exists.txt"), []byte("old\n"), 0644)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "exists.txt",
		Content:      "new\n",
		MustNotExist: true,
	})
	if err == nil {
		t.Fatal("expected error for must_not_exist on existing file")
	}
}

func TestFSWrite_OverwriteWithHash(t *testing.T) {
	r, root := newWriteRunner(t)
	original := "original content\n"
	_ = os.WriteFile(filepath.Join(root, "file.txt"), []byte(original), 0644)

	fileHash := cache.ComputeSHA256([]byte(original))

	resp, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:     "file.txt",
		Content:  "updated content\n",
		FileHash: fileHash,
	})
	if err != nil {
		t.Fatalf("FSWrite overwrite: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(root, "file.txt"))
	if string(data) != "updated content\n" {
		t.Errorf("unexpected content after overwrite: %q", string(data))
	}
	want := cache.ComputeSHA256([]byte("updated content\n"))
	if resp.FileHash != want {
		t.Errorf("file_hash after overwrite: got %s, want %s", resp.FileHash, want)
	}
}

func TestFSWrite_OverwriteWithStaleHash(t *testing.T) {
	r, root := newWriteRunner(t)
	_ = os.WriteFile(filepath.Join(root, "file.txt"), []byte("current\n"), 0644)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:     "file.txt",
		Content:  "new\n",
		FileHash: "sha256:deadbeef00000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected StaleContent error for wrong hash")
	}
}

func TestFSWrite_NoCondition_ReturnsError(t *testing.T) {
	r, root := newWriteRunner(t)
	_ = os.WriteFile(filepath.Join(root, "file.txt"), []byte("old\n"), 0644)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:    "file.txt",
		Content: "content\n",
		// neither file_hash nor must_not_exist on existing file
	})
	if err == nil {
		t.Fatal("expected error when overwriting without file_hash")
	}
}

func TestFSWrite_InferMustNotExistForNewFile(t *testing.T) {
	r, root := newWriteRunner(t)

	resp, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:    "auto-create.txt",
		Content: "created\n",
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "auto-create.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "created\n" {
		t.Errorf("content: %q", data)
	}
	if resp.FileHash == "" {
		t.Fatal("expected file_hash in response")
	}
}

func TestFSWrite_EmptyPath(t *testing.T) {
	r, _ := newWriteRunner(t)
	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{Content: "x", MustNotExist: true})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSWrite_CreatesParentDirs(t *testing.T) {
	r, root := newWriteRunner(t)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "sub/dir/file.go",
		Content:      "package sub\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite with nested path: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "sub", "dir", "file.go"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "package sub\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestFSWrite_ForceDiagnosticsForTest(t *testing.T) {
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{
		ForceDiagnosticsForTest: []lsp.ToolDiagnostic{
			{StartLine: 2, StartCol: 1, Severity: "error", Message: "undefined: Bar"},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	resp, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "a.go",
		Content:      "package main\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}
	if len(resp.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(resp.Diagnostics))
	}
	if resp.Diagnostics[0].Severity != "error" || resp.Diagnostics[0].Message != "undefined: Bar" {
		t.Errorf("unexpected diagnostic: %+v", resp.Diagnostics[0])
	}
}

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/cache"
)

func newWriteRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSWrite_CreateNewFile(t *testing.T) {
	r, root := newWriteRunner(t)

	resp, err := r.FSWrite(context.Background(), FSWriteRequest{
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

	_, err := r.FSWrite(context.Background(), FSWriteRequest{
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

	resp, err := r.FSWrite(context.Background(), FSWriteRequest{
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

	_, err := r.FSWrite(context.Background(), FSWriteRequest{
		Path:     "file.txt",
		Content:  "new\n",
		FileHash: "sha256:deadbeef00000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected StaleContent error for wrong hash")
	}
}

func TestFSWrite_NoCondition_ReturnsError(t *testing.T) {
	r, _ := newWriteRunner(t)

	_, err := r.FSWrite(context.Background(), FSWriteRequest{
		Path:    "file.txt",
		Content: "content\n",
		// neither file_hash nor must_not_exist
	})
	if err == nil {
		t.Fatal("expected error when no safety condition provided")
	}
}

func TestFSWrite_EmptyPath(t *testing.T) {
	r, _ := newWriteRunner(t)
	_, err := r.FSWrite(context.Background(), FSWriteRequest{Content: "x", MustNotExist: true})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSWrite_CreatesParentDirs(t *testing.T) {
	r, root := newWriteRunner(t)

	_, err := r.FSWrite(context.Background(), FSWriteRequest{
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

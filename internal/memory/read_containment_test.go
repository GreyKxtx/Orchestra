package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// memory_read takes a model-supplied path. It may read files under
// .orchestra/memory/ and nothing else: not through "..", not through a symlink.
func TestReadByPath_StaysInTheMemoryDirectory(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(outside, "ws")
	memDir := filepath.Join(root, ".orchestra", "memory")
	if err := os.MkdirAll(filepath.Join(memDir, "lessons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "agent.md"), []byte("a fact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".orchestra.env"), []byte("KEY=sk-live"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(root, "", Config{})

	if got := s.Read("", ".orchestra/memory/agent.md", 4096); got.Content != "a fact" {
		t.Fatalf("a memory file must still read: %+v", got)
	}

	for _, p := range []string{
		".orchestra/memory/../../secret.txt",
		".orchestra/memory/../../../secret.txt",
		".orchestra/memory/../../.orchestra.env",
		".orchestra/memory/lessons/../../../.orchestra.env",
		".orchestra/memory/./agent.md/../../../.orchestra.env",
	} {
		got := s.Read("", p, 4096)
		if strings.Contains(got.Content, "TOPSECRET") || strings.Contains(got.Content, "sk-live") ||
			!strings.HasPrefix(got.Content, "error:") {
			t.Fatalf("%s must be refused, got %+v", p, got)
		}
	}

	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	if err := os.Symlink(secret, filepath.Join(memDir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if got := s.Read("", ".orchestra/memory/link.md", 4096); strings.Contains(got.Content, "TOPSECRET") {
		t.Fatalf("a symlink out of the memory directory must be refused, got %+v", got)
	}
}

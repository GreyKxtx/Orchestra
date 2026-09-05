package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memory_read already reaches ~/.orchestra/memory.md (layer=global), but
// memory_write had no way to put anything there — a preference the user
// states ("I prefer TDD", "always use tabs") had nowhere to go but
// scope=project, which ties it to one repo instead of following the user.

func TestAppend_GlobalScope_WritesToHomeMemoryFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	projectRoot := t.TempDir()

	rel, n, err := NewStore(projectRoot, "", DefaultConfig()).Append("global", "prefers TDD, red before green")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("written byte count must be non-zero")
	}
	if rel != "~/.orchestra/memory.md" {
		t.Errorf("rel path = %q, want ~/.orchestra/memory.md", rel)
	}

	data, err := os.ReadFile(filepath.Join(home, ".orchestra", "memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "prefers TDD") {
		t.Errorf("global memory file missing content: %q", data)
	}

	// Must never leak into the project's own agent.md.
	if _, err := os.Stat(filepath.Join(projectRoot, ".orchestra", "memory", "agent.md")); !os.IsNotExist(err) {
		t.Error("scope=global must not also write agent.md")
	}
}

func TestAppend_GlobalScope_DisabledReturnsError(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	cfg := DefaultConfig()
	cfg.GlobalEnabled = false

	_, _, err := NewStore(t.TempDir(), "", cfg).Append("global", "prefers TDD")
	if err == nil {
		t.Fatal("global scope must refuse to write when global memory is disabled")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".orchestra", "memory.md")); !os.IsNotExist(statErr) {
		t.Error("must not create the file when disabled")
	}
}

func TestAppend_GlobalScope_RoundTripsThroughRead(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	store := NewStore(t.TempDir(), "", DefaultConfig())

	if _, _, err := store.Append("global", "always uses tabs, not spaces"); err != nil {
		t.Fatal(err)
	}

	res := store.Read("global", "", 4096)
	if !strings.Contains(res.Content, "always uses tabs") {
		t.Errorf("memory_read layer=global must see what memory_write scope=global wrote, got: %q", res.Content)
	}
}

package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
)

// initWorkspace makes a directory the registry will accept: core.New loads
// .orchestra.yml and fails without it.
func initWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return root
}

func TestRegistry_TwoProjectsHaveIndependentCores(t *testing.T) {
	a := initWorkspace(t)
	b := initWorkspace(t)

	r := NewRegistry(core.Options{})
	// defer, not t.Cleanup: cleanups run LIFO, so a t.Cleanup registered before
	// a later t.TempDir() would close the cores AFTER the directories are
	// removed — and Windows refuses to delete an open ckg.db.
	defer r.Shutdown()

	pa, err := r.Open(context.Background(), a)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	pb, err := r.Open(context.Background(), b)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}

	if pa.ID == pb.ID {
		t.Fatalf("two workspaces got the same project id: %q", pa.ID)
	}
	ca, ok := r.Get(pa.ID)
	if !ok {
		t.Fatal("core a missing from registry")
	}
	cb, ok := r.Get(pb.ID)
	if !ok {
		t.Fatal("core b missing from registry")
	}
	if ca == cb {
		t.Fatal("both projects resolved to the SAME core — they would share sessions and MCP prompts")
	}
	if got := ca.Health().WorkspaceRoot; got != a {
		t.Fatalf("core a workspace = %q, want %q", got, a)
	}
	if got := cb.Health().WorkspaceRoot; got != b {
		t.Fatalf("core b workspace = %q, want %q", got, b)
	}
	if n := len(r.List()); n != 2 {
		t.Fatalf("List() = %d projects, want 2", n)
	}
}

func TestRegistry_OpenTwiceIsRejected(t *testing.T) {
	a := initWorkspace(t)
	r := NewRegistry(core.Options{})
	// defer, not t.Cleanup: cleanups run LIFO, so a t.Cleanup registered before
	// a later t.TempDir() would close the cores AFTER the directories are
	// removed — and Windows refuses to delete an open ckg.db.
	defer r.Shutdown()

	if _, err := r.Open(context.Background(), a); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := r.Open(context.Background(), a); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("second open err = %v, want ErrAlreadyOpen", err)
	}
	if n := len(r.List()); n != 1 {
		t.Fatalf("a rejected re-open still changed the registry: %d entries", n)
	}
}

func TestRegistry_CloseFreesTheProject(t *testing.T) {
	a := initWorkspace(t)
	r := NewRegistry(core.Options{})
	// defer, not t.Cleanup: cleanups run LIFO, so a t.Cleanup registered before
	// a later t.TempDir() would close the cores AFTER the directories are
	// removed — and Windows refuses to delete an open ckg.db.
	defer r.Shutdown()

	p, err := r.Open(context.Background(), a)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := r.Close(p.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := r.Get(p.ID); ok {
		t.Fatal("core still resolvable after Close — its CKG database and language servers leak")
	}
	if n := len(r.List()); n != 0 {
		t.Fatalf("List() = %d after close, want 0", n)
	}
	// The same path can be opened again, which is what makes close useful.
	if _, err := r.Open(context.Background(), a); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
}

func TestRegistry_OpenFailuresAreTypedAndDoNotPoisonTheRegistry(t *testing.T) {
	r := NewRegistry(core.Options{})
	// defer, not t.Cleanup: cleanups run LIFO, so a t.Cleanup registered before
	// a later t.TempDir() would close the cores AFTER the directories are
	// removed — and Windows refuses to delete an open ckg.db.
	defer r.Shutdown()

	missing := filepath.Join(t.TempDir(), "definitely-not-here")
	if _, err := r.Open(context.Background(), missing); !errors.Is(err, ErrNoSuchDir) {
		t.Fatalf("missing dir err = %v, want ErrNoSuchDir", err)
	}

	bare := t.TempDir() // exists, but no .orchestra.yml
	if _, err := r.Open(context.Background(), bare); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("uninitialised dir err = %v, want ErrNotInitialized", err)
	}

	// A good project still opens afterwards: one bad path must not break the rest.
	good := initWorkspace(t)
	if _, err := r.Open(context.Background(), good); err != nil {
		t.Fatalf("open after two failures: %v", err)
	}
	if n := len(r.List()); n != 1 {
		t.Fatalf("List() = %d, want 1 — failed opens must not be listed", n)
	}
}

func TestRegistry_NameIsTheDirectoryName(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "my-repo")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	r := NewRegistry(core.Options{})
	// defer, not t.Cleanup: cleanups run LIFO, so a t.Cleanup registered before
	// a later t.TempDir() would close the cores AFTER the directories are
	// removed — and Windows refuses to delete an open ckg.db.
	defer r.Shutdown()
	p, err := r.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if p.Name != "my-repo" {
		t.Fatalf("Name = %q, want %q", p.Name, "my-repo")
	}
	if p.State != StateReady {
		t.Fatalf("State = %q, want ready", p.State)
	}
	if p.OpenedAt == 0 {
		t.Fatal("OpenedAt was never set")
	}
}

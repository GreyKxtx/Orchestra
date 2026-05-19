package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestSnapshotAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.AppendHistory([]llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "hello"},
	})
	s.SetPending([]ops.AnyOp{{Op: ops.OpFileWriteAtomic}})
	s.SetTodos([]tools.TodoItem{{Content: "a", Status: "pending"}})

	if err := s.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	loaded, err := LoadFromDisk(dir, s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ID != s.ID {
		t.Errorf("ID: got %q, want %q", loaded.ID, s.ID)
	}
	if len(loaded.History) != 2 {
		t.Errorf("History len: got %d, want 2", len(loaded.History))
	}
	if !loaded.HasPending() {
		t.Error("pending ops lost")
	}
	if len(loaded.CopyTodos()) != 1 {
		t.Errorf("Todos: got %d, want 1", len(loaded.CopyTodos()))
	}
}

func TestLoadFromDisk_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFromDisk(dir, "does-not-exist")
	if !os.IsNotExist(err) {
		t.Errorf("want os.IsNotExist, got %v", err)
	}
}

func TestLoadOrCreate_HitsCacheThenDiskThenFresh(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()

	// 1) Fresh — no snapshot, no in-memory entry. Should create a
	// session with the requested ID so client bookkeeping survives.
	s1, err := m.LoadOrCreate(dir, "abc123")
	if err != nil {
		t.Fatalf("LoadOrCreate fresh: %v", err)
	}
	if s1.ID != "abc123" {
		t.Errorf("fresh ID: got %q, want abc123", s1.ID)
	}

	// 2) Cached in memory — second call returns the same pointer.
	s2, err := m.LoadOrCreate(dir, "abc123")
	if err != nil {
		t.Fatalf("LoadOrCreate cached: %v", err)
	}
	if s2 != s1 {
		t.Error("cached path returned a different pointer")
	}

	// 3) From disk after a "core restart": snapshot the in-memory
	// session, then evict from manager and reload.
	s1.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "persisted"}})
	if err := s1.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	m2 := NewManager() // simulates a fresh core
	loaded, err := m2.LoadOrCreate(dir, "abc123")
	if err != nil {
		t.Fatalf("LoadOrCreate after restart: %v", err)
	}
	if len(loaded.History) != 1 {
		t.Errorf("post-restart History: got %d, want 1", len(loaded.History))
	}
}

func TestSnapshot_AtomicAndIsolated(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "x"}})
	if err := s.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// Verify file exists at the expected path.
	want := filepath.Join(dir, ".orchestra", "sessions", s.ID+".json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("snapshot file missing: %v", err)
	}
}

func TestSnapshot_EmptyWorkspaceNoOp(t *testing.T) {
	s := New()
	if err := s.Snapshot(""); err != nil {
		t.Errorf("empty workspace_root should be a no-op, got %v", err)
	}
}

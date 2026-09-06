package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/internal/sessionfile"
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

// TestSnapshotAndLoad_RoundTripsTurnStarts: turn boundaries are the only link
// between UIMessages and History, so dropping them on a save/load would make
// fork and rewind refuse on every session that has ever been reopened.
func TestSnapshotAndLoad_RoundTripsTurnStarts(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.AppendTurnStart(0)
	s.AppendHistory([]llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleTool, Content: "tool"},
	})
	s.AppendTurnStart(2)
	s.AppendHistory([]llm.Message{{Role: llm.RoleAssistant, Content: "a2"}})

	if err := s.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Present in the file itself, under the documented key.
	raw, err := os.ReadFile(filepath.Join(dir, ".orchestra", "sessions", s.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		TurnStarts []int `json:"turn_starts"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk.TurnStarts) != 2 || onDisk.TurnStarts[0] != 0 || onDisk.TurnStarts[1] != 2 {
		t.Fatalf("turn_starts on disk = %v, want [0 2]", onDisk.TurnStarts)
	}

	loaded, err := LoadFromDisk(dir, s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := loaded.TurnStarts()
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("TurnStarts after load = %v, want [0 2]", got)
	}
}

// TestSnapshotAndLoad_RoundTripsLineage: a branch's parent_id and
// forked_from_index are written by fork, but the branch's first save after
// session.start goes through toSnapshot — which must not drop them.
func TestSnapshotAndLoad_RoundTripsLineage(t *testing.T) {
	dir := t.TempDir()
	snap := &sessionfile.Snapshot{
		Version:         sessionfile.Version,
		ID:              sessionfile.NewID(),
		History:         []llm.Message{},
		UIMessages:      []sessionfile.UIMessage{},
		ParentID:        "20260101T000000-aaaa",
		ForkedFromIndex: 4,
	}
	if err := sessionfile.Save(dir, snap); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFromDisk(dir, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	again, err := sessionfile.Load(dir, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParentID != "20260101T000000-aaaa" || again.ForkedFromIndex != 4 {
		t.Fatalf("lineage lost on re-save: parent=%q index=%d", again.ParentID, again.ForkedFromIndex)
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
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var hdr struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &hdr); err != nil {
		t.Fatalf("decode snapshot header: %v", err)
	}
	if hdr.Version != sessionfile.Version {
		t.Errorf("version: got %d, want %d", hdr.Version, sessionfile.Version)
	}
}

func TestSnapshot_EmptyWorkspaceNoOp(t *testing.T) {
	s := New()
	if err := s.Snapshot(""); err != nil {
		t.Errorf("empty workspace_root should be a no-op, got %v", err)
	}
}

// TestGetOrLoad_ThreeBranches: in-memory hit → no disk read, disk hit
// → rehydrated and cached, miss → "session not found" error preserved
// from the original Manager.Get contract so handlers can still reject
// unknown IDs.
func TestGetOrLoad_ThreeBranches(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()

	// (1) In-memory branch.
	created := m.Create()
	got, err := m.GetOrLoad(dir, created.ID)
	if err != nil {
		t.Fatalf("in-memory: %v", err)
	}
	if got != created {
		t.Error("in-memory: expected same pointer")
	}

	// (2) Disk branch — snapshot, evict via fresh manager, reload.
	created.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "persisted"}})
	if err := created.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	m2 := NewManager()
	loaded, err := m2.GetOrLoad(dir, created.ID)
	if err != nil {
		t.Fatalf("from disk: %v", err)
	}
	if len(loaded.History) != 1 {
		t.Errorf("from disk: history len = %d, want 1", len(loaded.History))
	}
	// Second call must hit the in-memory cache, not disk again.
	again, err := m2.GetOrLoad(dir, created.ID)
	if err != nil {
		t.Fatalf("cached after load: %v", err)
	}
	if again != loaded {
		t.Error("cached after load: expected same pointer")
	}

	// (3) Miss branch — strict "session not found" so SessionMessage
	// keeps rejecting unknown IDs.
	if _, err := m2.GetOrLoad(dir, "nope"); err == nil {
		t.Error("miss: expected error, got nil")
	}
}

// TestDeleteSnapshot_RemovesFile: SessionClose must remove the
// on-disk snapshot too, otherwise closed sessions resurrect after
// core restart.
func TestDeleteSnapshot_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	s := New()
	s.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "x"}})
	if err := s.Snapshot(dir); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	path := filepath.Join(dir, ".orchestra", "sessions", s.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("precondition: snapshot missing: %v", err)
	}
	if err := DeleteSnapshot(dir, s.ID); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("snapshot still present after DeleteSnapshot: err=%v", err)
	}
	// Idempotent — second call must not error.
	if err := DeleteSnapshot(dir, s.ID); err != nil {
		t.Errorf("second DeleteSnapshot should be a no-op, got %v", err)
	}
}

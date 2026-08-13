package session

import (
	"context"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// TestRefreshFromDiskIfNewer_PicksUpExternalWrite: simulates a detached
// background core finishing a turn — a *different* Session instance writes a
// newer snapshot, and the cached manager copy must pick it up.
func TestRefreshFromDiskIfNewer_PicksUpExternalWrite(t *testing.T) {
	dir := t.TempDir()

	// "Foreground" core: session cached in the manager.
	seed := NewWithID("refresh-1")
	seed.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "q1"}})
	seed.Lock()
	if err := seed.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	seed.Unlock()

	m := NewManager()
	cached, err := m.GetOrLoad(dir, "refresh-1")
	if err != nil {
		t.Fatal(err)
	}

	// Nothing new on disk yet → no refresh.
	if m.RefreshFromDiskIfNewer(dir, "refresh-1") {
		t.Fatal("refresh with no external write should be a no-op")
	}

	// "Background" core: separate instance loads, appends the finished turn,
	// snapshots. Sleep past the 1s snapshot timestamp granularity edge.
	time.Sleep(20 * time.Millisecond)
	other, err := LoadFromDisk(dir, "refresh-1")
	if err != nil {
		t.Fatal(err)
	}
	other.AppendHistory([]llm.Message{{Role: llm.RoleAssistant, Content: "finished in background"}})
	other.Lock()
	if err := other.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	other.Unlock()

	if !m.RefreshFromDiskIfNewer(dir, "refresh-1") {
		t.Fatal("expected refresh after external write")
	}
	cached.Lock()
	n := len(cached.History)
	last := ""
	if n > 0 {
		last = cached.History[n-1].Content
	}
	cached.Unlock()
	if n != 2 || last != "finished in background" {
		t.Fatalf("history not refreshed: len=%d last=%q", n, last)
	}

	// Idempotent: same disk version again → no-op.
	if m.RefreshFromDiskIfNewer(dir, "refresh-1") {
		t.Fatal("second refresh with same disk state should be a no-op")
	}
}

// TestRefreshFromDiskIfNewer_SkipsBusySession: a running local turn must
// never be clobbered by a disk reload.
func TestRefreshFromDiskIfNewer_SkipsBusySession(t *testing.T) {
	dir := t.TempDir()
	seed := NewWithID("refresh-busy")
	seed.Lock()
	if err := seed.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	seed.Unlock()

	m := NewManager()
	cached, err := m.GetOrLoad(dir, "refresh-busy")
	if err != nil {
		t.Fatal(err)
	}

	// External newer write.
	time.Sleep(20 * time.Millisecond)
	other, err := LoadFromDisk(dir, "refresh-busy")
	if err != nil {
		t.Fatal(err)
	}
	other.AppendHistory([]llm.Message{{Role: llm.RoleAssistant, Content: "external"}})
	other.Lock()
	if err := other.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	other.Unlock()

	// Mark the cached copy busy (turn in flight).
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	cached.Lock()
	cached.SetCancel(cancel)
	cached.Unlock()

	if m.RefreshFromDiskIfNewer(dir, "refresh-busy") {
		t.Fatal("busy session must not be refreshed")
	}

	cached.Lock()
	cached.ClearCancel()
	cached.Unlock()
	if !m.RefreshFromDiskIfNewer(dir, "refresh-busy") {
		t.Fatal("refresh should proceed once the session is idle")
	}
}

// TestRefreshFromDiskIfNewer_OwnSnapshotIgnored: our own Snapshot() writes
// must not be mistaken for external updates.
func TestRefreshFromDiskIfNewer_OwnSnapshotIgnored(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	s := m.Create()
	s.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "hi"}})
	s.Lock()
	if err := s.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	s.Unlock()

	if m.RefreshFromDiskIfNewer(dir, s.ID) {
		t.Fatal("own snapshot must not trigger a refresh")
	}
}

package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

// session.start prunes the snapshot store to retention.sessions; the session
// just started and every session this core holds are kept.
func TestSessionStart_PrunesSnapshotsPastRetention(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"a-old", "b-mid", "c-new"} {
		if err := sessionfile.Save(root, &sessionfile.Snapshot{ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig(root)
	cfg.Retention.Sessions = 1
	c, err := New(root, Options{Config: cfg, ToolsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	res, err := c.SessionStart(SessionStartParams{SessionID: "a-old"})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "a-old" {
		t.Fatalf("SessionID = %q", res.SessionID)
	}
	metas, err := sessionfile.ListMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	// The newest ("c-new") stays as the one kept; "a-old" is held by this
	// core; "b-mid" is what retention removes.
	got := map[string]bool{}
	for _, m := range metas {
		got[m.ID] = true
	}
	if !got["c-new"] || !got["a-old"] || got["b-mid"] || len(metas) != 2 {
		t.Fatalf("sessions left = %v, want c-new and the held a-old", got)
	}
}

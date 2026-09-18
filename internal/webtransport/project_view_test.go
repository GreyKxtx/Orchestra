package webtransport

import (
	"os"
	"path/filepath"
	"testing"
)

// The rail draws each remembered project with a session count, and shows a
// folder that is gone as such instead of as a row that 404s when clicked. Both
// come from one bounded pass over paths that may be on an unreachable share,
// so neither may be assumed and neither may block.
func TestInspectPaths_CountsSessionsAndSpotsAMissingFolder(t *testing.T) {
	dir := t.TempDir()

	withSessions := filepath.Join(dir, "with-sessions")
	sessions := filepath.Join(withSessions, ".orchestra", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.json", "b.json", "c.events.jsonl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(sessions, name), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// A directory among them must not be counted as a session.
	if err := os.MkdirAll(filepath.Join(sessions, "d.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fresh := filepath.Join(dir, "fresh") // opened, never used
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	gone := filepath.Join(dir, "gone") // remembered, then deleted

	got := inspectPaths([]string{withSessions, fresh, gone})

	if f := got[withSessions]; f.sessions != 2 {
		t.Errorf("sessions = %d, want 2 — only *.json files, and no directories", f.sessions)
	}
	if f := got[withSessions]; f.missing {
		t.Error("a project with sessions was reported as missing")
	}
	if f := got[fresh]; f.sessions != 0 || f.missing {
		t.Errorf("a project with no sessions = %+v, want {sessions:0 missing:false}", f)
	}
	if f := got[gone]; !f.missing {
		t.Errorf("a deleted folder = %+v, want missing", f)
	}
	if f := got[gone]; f.sessions != -1 {
		t.Errorf("a deleted folder reported %d sessions; -1 is what the rail draws as no count", f.sessions)
	}
}

// The empty case must not deadlock on a channel nobody writes to.
func TestInspectPaths_NoPaths(t *testing.T) {
	if got := inspectPaths(nil); len(got) != 0 {
		t.Fatalf("inspectPaths(nil) = %v, want empty", got)
	}
}

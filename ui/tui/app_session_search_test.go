package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestSessionsMatchingQuery_FiltersByMessageText(t *testing.T) {
	a := testChromeApp(t)
	// testChromeApp builds an App with Config{Model, Mode, CWD} only
	// (ui/tui/app_chrome_test.go:11-22), so WorkspaceRoot is empty and every
	// session read/write would land outside a temp dir. Point it at one.
	root := t.TempDir()
	a.cfg.WorkspaceRoot = root

	save := func(id, text string) {
		t.Helper()
		snap := &sessionfile.Snapshot{
			Version:    sessionfile.Version,
			ID:         id,
			Title:      id,
			UIMessages: []sessionfile.UIMessage{{Role: "user", Text: text}},
		}
		if err := sessionfile.Save(root, snap); err != nil {
			t.Fatal(err)
		}
	}
	save("20260901T100000-aaaa", "wire the bearer token")
	save("20260902T100000-bbbb", "something else entirely")

	metas, err := a.sessionsMatchingQuery("bearer")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("metas = %d, want 1: %+v", len(metas), metas)
	}
	if metas[0].ID != "20260901T100000-aaaa" {
		t.Fatalf("ID = %q", metas[0].ID)
	}
}

func TestSessionsMatchingQuery_NoMatchesIsNotAnError(t *testing.T) {
	a := testChromeApp(t)
	a.cfg.WorkspaceRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(a.cfg.WorkspaceRoot, ".orchestra", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	metas, err := a.sessionsMatchingQuery("nothing-matches-this")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(metas) != 0 {
		t.Fatalf("metas = %+v, want none", metas)
	}
}

func TestParseSessionsQuery(t *testing.T) {
	for _, tc := range []struct {
		in    string
		query string
		ok    bool
	}{
		{"/sessions bearer token", "bearer token", true},
		{"/sessions   spaced  ", "spaced", true},
		{"/sessions", "", false}, // the bare form keeps its existing path
		{"/sessionsfoo", "", false},
		{"/rewind", "", false},
	} {
		query, ok := parseSessionsQuery(tc.in)
		if ok != tc.ok || query != tc.query {
			t.Errorf("parseSessionsQuery(%q) = (%q,%v), want (%q,%v)", tc.in, query, ok, tc.query, tc.ok)
		}
	}
}

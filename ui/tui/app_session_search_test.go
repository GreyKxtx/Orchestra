package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// saveSessionFixture writes a session file directly, bypassing
// sessionfile.Save, because Save stamps UpdatedAt with time.Now() and tests
// that assert on ordering by update time need to control it. Mirrors
// internal/sessionfile/search_test.go's saveSearchFixture.
func saveSessionFixture(t *testing.T, root, id, title string, updated time.Time, msgs []sessionfile.UIMessage) {
	t.Helper()
	snap := &sessionfile.Snapshot{
		Version:    sessionfile.Version,
		ID:         id,
		Title:      title,
		UIMessages: msgs,
		CreatedAt:  updated,
		UpdatedAt:  updated,
		MsgCount:   len(msgs),
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".orchestra", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

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
	// No .orchestra/sessions directory is created here on purpose:
	// sessionfile.Search already treats a missing sessions directory as "no
	// hits, no error", so creating one first would just be dead weight.
	metas, err := a.sessionsMatchingQuery("nothing-matches-this")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(metas) != 0 {
		t.Fatalf("metas = %+v, want none", metas)
	}
}

// TestSessionsMatchingQuery_DedupesMultiHitSessionsAndOrdersByRecency covers
// two guarantees a single-match test cannot: a session with several matching
// messages must appear exactly once (not once per hit), and the result order
// must follow sessionstore.List's most-recently-updated-first order, not
// incidental directory-scan (alphabetical filename) order. "aaaa" sorts
// before "bbbb" so os.ReadDir visits it first, but it is the *older* of the
// two sessions here — a regression that leaked directory-scan order instead
// of recency order would put it first and this test would catch it.
func TestSessionsMatchingQuery_DedupesMultiHitSessionsAndOrdersByRecency(t *testing.T) {
	a := testChromeApp(t)
	root := t.TempDir()
	a.cfg.WorkspaceRoot = root

	older := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

	saveSessionFixture(t, root, "20260901T100000-aaaa", "multi", older, []sessionfile.UIMessage{
		{Role: "user", Text: "token one"},
		{Role: "user", Text: "token two"},
		{Role: "user", Text: "token three"},
	})
	saveSessionFixture(t, root, "20260902T100000-bbbb", "single", newer, []sessionfile.UIMessage{
		{Role: "user", Text: "token four"},
	})

	metas, err := a.sessionsMatchingQuery("token")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("metas = %d, want 2 (one per session, not one per hit): %+v", len(metas), metas)
	}
	if metas[0].ID != "20260902T100000-bbbb" || metas[1].ID != "20260901T100000-aaaa" {
		t.Fatalf("order = [%s, %s], want most-recently-updated first: [bbbb, aaaa]",
			metas[0].ID, metas[1].ID)
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
		{"/sessions", "", false},    // the bare form keeps its existing path
		{"/sessions   ", "", false}, // whitespace-only argument is still "no query"
		{"/sessionsfoo", "", false},
		{"/rewind", "", false},
	} {
		query, ok := parseSessionsQuery(tc.in)
		if ok != tc.ok || query != tc.query {
			t.Errorf("parseSessionsQuery(%q) = (%q,%v), want (%q,%v)", tc.in, query, ok, tc.query, tc.ok)
		}
	}
}

// The filtered picker must render the same columns as the unfiltered one.
// dialog_sessions.go:155-160 draws "· N msgs" and "· model" only when they are
// non-zero, so metas built from search hits that dropped them lose two columns
// silently — the rows just look different from the ones /sessions shows.
func TestSessionsMatchingQuery_KeepsTheColumnsThePickerRenders(t *testing.T) {
	a := testChromeApp(t)
	root := t.TempDir()
	a.cfg.WorkspaceRoot = root

	saveSessionFixtureWithModel(t, root, "20260901T100000-aaaa", "wiring auth", "claude-opus-5",
		time.Now().Add(-time.Hour), []sessionfile.UIMessage{
			{Role: "user", Text: "wire the bearer token"},
			{Role: "assistant", Text: "authTransport sets the header"},
			{Role: "user", Text: "thanks"},
		})

	metas, err := a.sessionsMatchingQuery("bearer")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("metas = %d, want 1", len(metas))
	}
	if metas[0].MsgCount != 3 {
		t.Errorf("MsgCount = %d, want 3 — the filtered row drops \"· N msgs\"", metas[0].MsgCount)
	}
	if metas[0].Model != "claude-opus-5" {
		t.Errorf("Model = %q, want %q — the filtered row drops the model",
			metas[0].Model, "claude-opus-5")
	}
}

// saveSessionFixtureWithModel is saveSessionFixture plus the model field, which
// the picker renders and Save does not invent.
func saveSessionFixtureWithModel(t *testing.T, root, id, title, model string, updated time.Time, msgs []sessionfile.UIMessage) {
	t.Helper()
	snap := &sessionfile.Snapshot{
		Version:    sessionfile.Version,
		ID:         id,
		Title:      title,
		Model:      model,
		UIMessages: msgs,
		CreatedAt:  updated,
		UpdatedAt:  updated,
		MsgCount:   len(msgs),
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".orchestra", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

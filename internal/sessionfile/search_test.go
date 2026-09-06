package sessionfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// saveSearchFixture writes one session file directly rather than through Save,
// because Save stamps UpdatedAt with time.Now() and these tests assert on
// ordering by update time.
func saveSearchFixture(t *testing.T, root, id, title string, updated time.Time, msgs []UIMessage) {
	t.Helper()
	snap := &Snapshot{
		Version:    Version,
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

func TestSearch_FindsAMessageAndReportsItsIndex(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "first", time.Now().Add(-time.Hour), []UIMessage{
		{Role: "user", Text: "how do I wire the bearer token"},
		{Role: "assistant", Text: "authTransport sets the header"},
	})

	hits, err := Search(root, SearchOptions{Query: "bearer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.SessionID != "20260901T100000-aaaa" {
		t.Errorf("SessionID = %q", h.SessionID)
	}
	// The index is what `session fork --at` and session.rewind both take.
	if h.Index != 0 {
		t.Errorf("Index = %d, want 0", h.Index)
	}
	if h.Role != "user" {
		t.Errorf("Role = %q, want user", h.Role)
	}
	if !strings.Contains(h.Snippet, "bearer") {
		t.Errorf("Snippet = %q, want it to carry the match", h.Snippet)
	}
}

func TestSearch_IsCaseSensitiveByDefaultAndInsensitiveOnRequest(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{Role: "user", Text: "Bearer token"},
	})

	// Mirrors `orchestra search`, which defaults to case-sensitive (internal/cli/search.go:27).
	if hits, err := Search(root, SearchOptions{Query: "bearer"}); err != nil || len(hits) != 0 {
		t.Fatalf("case-sensitive search matched anyway: %v %+v", err, hits)
	}
	hits, err := Search(root, SearchOptions{Query: "bearer", Insensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("insensitive hits = %d, want 1", len(hits))
	}
}

func TestSearch_SkipsToolBlocksUnlessIncludeAll(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{
			Role: "assistant",
			Text: "done",
			ToolBlocks: []UIToolBlock{
				{Name: "read", Result: "package remote contains authTransport"},
			},
		},
	})

	// Tool output is large and noisy; by default it must not bury prose hits.
	if hits, err := Search(root, SearchOptions{Query: "authTransport"}); err != nil || len(hits) != 0 {
		t.Fatalf("default search reached tool output: %v %+v", err, hits)
	}
	hits, err := Search(root, SearchOptions{Query: "authTransport", IncludeAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("--all hits = %d, want 1", len(hits))
	}
}

func TestSearch_OneHitPerMessage(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{Role: "user", Text: "token token token token"},
	})

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatal(err)
	}
	// Four occurrences in one message must not become four rows.
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
}

func TestSearch_OrdersRecentSessionsFirstAndCapsWithLimit(t *testing.T) {
	root := t.TempDir()
	older := time.Now().Add(-48 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	saveSearchFixture(t, root, "20260901T100000-aaaa", "old", older, []UIMessage{
		{Role: "user", Text: "token a"},
		{Role: "user", Text: "token b"},
	})
	saveSearchFixture(t, root, "20260903T100000-bbbb", "new", newer, []UIMessage{
		{Role: "user", Text: "token c"},
	})

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	if hits[0].SessionID != "20260903T100000-bbbb" {
		t.Errorf("most recently updated session must come first, got %q", hits[0].SessionID)
	}
	// Within a session, message order is ascending.
	if hits[1].Index != 0 || hits[2].Index != 1 {
		t.Errorf("in-session order = %d,%d, want 0,1", hits[1].Index, hits[2].Index)
	}

	capped, err := Search(root, SearchOptions{Query: "token", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 2 {
		t.Fatalf("limited hits = %d, want 2", len(capped))
	}
	if capped[0].SessionID != "20260903T100000-bbbb" {
		t.Errorf("the cap must keep the most recent sessions, got %q", capped[0].SessionID)
	}
}

func TestSearch_SkipsUnreadableFilesInsteadOfFailing(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "good", time.Now(), []UIMessage{
		{Role: "user", Text: "token here"},
	})
	// One corrupt file must not take the other fifty-one down with it.
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "sessions", "broken.json"),
		[]byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatalf("a corrupt file must not fail the search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
}

func TestSearch_EmptyQueryAndMissingDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := Search(root, SearchOptions{Query: "  "}); err == nil {
		t.Error("an empty query must be refused")
	}
	// A project that has never had a session is not an error.
	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatalf("missing sessions dir must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v, want none", hits)
	}
}

func TestSnippetAround_TrimsAndMarksElision(t *testing.T) {
	long := strings.Repeat("a ", 200) + "needle " + strings.Repeat("b ", 200)
	got := snippetAround(long, "needle", false)
	if !strings.Contains(got, "needle") {
		t.Fatalf("snippet lost the match: %q", got)
	}
	if len([]rune(got)) > snippetWidth+2 {
		t.Fatalf("snippet is %d runes, want at most %d plus the two ellipses", len([]rune(got)), snippetWidth)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("both ends were cut, so both should be marked: %q", got)
	}
}

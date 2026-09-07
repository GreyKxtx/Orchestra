package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMemoryFile writes one memory layer file, creating its directory.
func writeMemoryFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Until now memory search existed only for the model (the memory_search tool).
// A user who wanted to know whether a fact was remembered had to open the
// files by hand.
func TestSearchMemoryLayers_FindsAnEntryAndNamesItsLayer(t *testing.T) {
	root := t.TempDir()
	writeMemoryFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"),
		"## 2026-09-01\ngoal: wire the bearer token through authTransport\n\n---\n\n## 2026-09-02\ngoal: unrelated\n")

	hits := searchMemoryLayers(root, "", "bearer token", 8)
	if len(hits) == 0 {
		t.Fatal("no hits for a phrase that is in agent.md")
	}
	if !strings.Contains(hits[0].Snippet, "bearer token") {
		t.Errorf("snippet = %q, must contain the match", hits[0].Snippet)
	}
	if hits[0].Layer == "" {
		t.Error("hit has no layer — a user cannot tell project memory from global without it")
	}
}

// A query matching nothing must come back empty rather than dumping the file:
// "not remembered" is the answer the user is asking for.
func TestSearchMemoryLayers_EmptyWhenNothingMatches(t *testing.T) {
	root := t.TempDir()
	writeMemoryFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"),
		"goal: something entirely different\n")

	if hits := searchMemoryLayers(root, "", "bearer token", 8); len(hits) != 0 {
		t.Fatalf("hits = %+v, want none", hits)
	}
}

// The limit is what keeps a broad query from flooding the chat pane.
func TestSearchMemoryLayers_RespectsTheLimit(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("## entry\ngoal: token work\n\n---\n\n")
	}
	writeMemoryFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"), b.String())

	hits := searchMemoryLayers(root, "", "token", 3)
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want exactly the 3 asked for", len(hits))
	}
}

// Search is case-insensitive, matching the model-side memory_search: a user
// typing lowercase must find a fact recorded with capitals.
func TestSearchMemoryLayers_IsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeMemoryFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"),
		"goal: OAuth Dynamic Client Registration\n")

	if hits := searchMemoryLayers(root, "", "dynamic client", 8); len(hits) == 0 {
		t.Fatal("lowercase query found nothing in a capitalised entry")
	}
}

func TestParseMemorySlashCommand_AcceptsSearchWithAMultiWordQuery(t *testing.T) {
	verb, arg, ok := parseMemorySlashCommand("/memory search bearer token")
	if !ok || verb != "search" {
		t.Fatalf("verb=%q ok=%v, want search/true", verb, ok)
	}
	if arg != "bearer token" {
		t.Errorf("arg = %q, want the whole query — a search that silently drops "+
			"every word after the first finds the wrong thing", arg)
	}
}

func TestParseMemorySlashCommand_RejectsSearchWithNoQuery(t *testing.T) {
	if _, _, ok := parseMemorySlashCommand("/memory search"); ok {
		t.Fatal("bare /memory search was accepted; there is nothing to search for")
	}
	if _, _, ok := parseMemorySlashCommand("/memory search    "); ok {
		t.Fatal("/memory search with only spaces was accepted")
	}
}

// The existing verbs must keep working exactly as they did — they are how the
// two-field parser was used before search widened it.
func TestParseMemorySlashCommand_KeepsOpenAndRefresh(t *testing.T) {
	for _, verb := range []string{"open", "refresh"} {
		got, arg, ok := parseMemorySlashCommand("/memory " + verb)
		if !ok || got != verb {
			t.Fatalf("/memory %s: verb=%q ok=%v", verb, got, ok)
		}
		if arg != "" {
			t.Errorf("/memory %s: arg = %q, want empty", verb, arg)
		}
	}
	// A bare /memory stays the view-only command handled elsewhere.
	if _, _, ok := parseMemorySlashCommand("/memory"); ok {
		t.Fatal("bare /memory was claimed here; it belongs to the exact-match switch")
	}
	if _, _, ok := parseMemorySlashCommand("/memory bogus"); ok {
		t.Fatal("an unknown verb was accepted")
	}
}

// Store.Read returns its failure modes as CONTENT, not as errors: the session
// layer with no active session answers with the literal string
// "no active session_id". Searching that string finds a hit that looks exactly
// like a remembered fact, attributed to a layer that does not exist yet.
func TestSearchMemoryLayers_DoesNotMatchTheEmptySessionSentinel(t *testing.T) {
	root := t.TempDir()
	writeMemoryFile(t, filepath.Join(root, ".orchestra", "memory", "agent.md"),
		"goal: something unrelated\n")

	for _, q := range []string{"no active session_id", "active session", "session_id"} {
		if hits := searchMemoryLayers(root, "", q, 8); len(hits) != 0 {
			t.Errorf("query %q matched the store's own placeholder text: %+v", q, hits)
		}
	}
}

package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionStore returns a store for one session id, sharing a workspace root,
// with that session's own memory file written.
func sessionStore(t *testing.T, root, sessionID string, entries ...string) *Store {
	t.Helper()
	cfg := Config{}
	cfg.Normalize()
	cfg.SessionEnabled = true
	s := NewStore(root, sessionID, cfg)

	path := filepath.Join(root, ".orchestra", "memory", "sessions", sessionID+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, len(entries))
	for i, e := range entries {
		bodies[i] = "*2026-09-07T00:00:00Z* [project]\n\n" + e
	}
	if err := os.WriteFile(path, []byte(strings.Join(bodies, entrySep)), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

// Memory is not one flat set: the session layer is per-session, so the chunks
// one search can see are only ever a subset of what the cache holds. Pruning
// everything the current search did not see therefore deletes the other
// sessions' vectors — and since both sessions keep searching, each one keeps
// re-embedding what the other just threw away. The cache would be worse than
// useless with two sessions open, which is the normal case.
func TestSemanticSearch_PruneDoesNotEvictAnotherSessionsVectors(t *testing.T) {
	root := t.TempDir()
	vs := newMemVectorStore()
	ctx := context.Background()

	a := sessionStore(t, root, "sess-a", "session A remembers the staging URL")
	b := sessionStore(t, root, "sess-b", "session B remembers the load test plan")

	if _, err := SemanticSearchIndexed(ctx, a, root, "url", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	if _, err := SemanticSearchIndexed(ctx, b, root, "plan", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}

	// Session A searches again. Its own chunk was embedded a moment ago and
	// has not changed, so nothing should go to the embedding endpoint but the
	// query.
	emb := &countingEmbedder{}
	if _, err := SemanticSearchIndexed(ctx, a, root, "url", 3, emb, vs); err != nil {
		t.Fatal(err)
	}
	got := emb.embedded()
	if len(got) != 1 {
		t.Fatalf("session A re-embedded %v after session B searched; B's prune evicted A's vectors", got)
	}
}

// The prune must still do its job within one session: a deleted entry's
// vector goes, and the other session's stays.
func TestSemanticSearch_PruneStillDropsThisSessionsDeletedChunks(t *testing.T) {
	root := t.TempDir()
	vs := newMemVectorStore()
	ctx := context.Background()

	a := sessionStore(t, root, "sess-a", "session A remembers the staging URL", "session A remembers the docker registry")
	b := sessionStore(t, root, "sess-b", "session B remembers the load test plan")

	if _, err := SemanticSearchIndexed(ctx, a, root, "url", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	if _, err := SemanticSearchIndexed(ctx, b, root, "plan", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	before := vs.count("test-embed")
	if before != 3 {
		t.Fatalf("stored %d vectors, want 3", before)
	}

	// A drops one of its entries.
	a = sessionStore(t, root, "sess-a", "session A remembers the staging URL")
	if _, err := SemanticSearchIndexed(ctx, a, root, "url", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	if got := vs.count("test-embed"); got != 2 {
		t.Fatalf("stored %d vectors after A deleted one, want 2 (A's live one + B's)", got)
	}
}

package ckg

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func memStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ckg.db")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestMemoryEmbeddings_SaveLoadRoundTrip(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	want := map[string][]float32{
		"h1": {0.5, -0.25, 1},
		"h2": {0, 1, 0},
	}
	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{
		{Hash: "h1", Scope: "ws", Vector: want["h1"]},
		{Hash: "h2", Scope: "ws", Vector: want["h2"]},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"h1", "h2", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The model is part of the key, not decoration: vectors from one embedding
// model are meaningless against another's, so switching embed.model must miss
// rather than silently rank against incompatible vectors.
func TestMemoryEmbeddings_LoadIsScopedToTheModel(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryEmbeddings(ctx, "other-model", []string{"h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v for a different model; vectors are not comparable across models", got)
	}
}

// Re-saving the same hash replaces the vector: a chunk whose hash collides
// after an edit must not keep the old vector.
func TestMemoryEmbeddings_SaveReplaces(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{0, 1}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"h1"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["h1"], []float32{0, 1}) {
		t.Fatalf("got %v, want the replacement vector", got["h1"])
	}
}

// Memory is edited, so hashes die. Without a prune the table grows forever
// with vectors for text that no longer exists anywhere.
func TestMemoryEmbeddings_PruneDropsVectorsForVanishedChunks(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{
		{Hash: "keep", Scope: "ws", Vector: []float32{1, 0}},
		{Hash: "gone", Scope: "ws", Vector: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryEmbeddings(ctx, "other", []MemoryVector{{Hash: "gone", Scope: "ws", Vector: []float32{1, 1}}}); err != nil {
		t.Fatal(err)
	}

	if err := s.PruneMemoryEmbeddings(ctx, "e5", []string{"ws"}, []string{"keep"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"keep", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, still := got["gone"]; still {
		t.Error("a vanished chunk kept its vector")
	}
	if _, ok := got["keep"]; !ok {
		t.Error("prune removed a live chunk")
	}

	// Another model's rows are not this model's business.
	other, err := s.LoadMemoryEmbeddings(ctx, "other", []string{"gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Error("prune for one model deleted another model's vectors")
	}
}

// An empty keep set means every chunk is gone, which is a legitimate state
// (memory cleared) — and must not be read as "keep everything".
func TestMemoryEmbeddings_PruneWithEmptyKeepClearsTheModel(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneMemoryEmbeddings(ctx, "e5", []string{"ws"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want everything cleared", got)
	}
}

// The code graph is a local cache: a schema bump drops and rescans it, which
// costs only CPU. Memory vectors are not — re-embedding them costs real calls
// to the embedding endpoint, and they do not depend on the code graph at all
// (their key is the chunk's content hash). So a migration that rebuilds the
// graph must leave them alone.
func TestMemoryEmbeddings_SurviveACodeGraphMigration(t *testing.T) {
	s, path := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Pretend this database was written by an older binary.
	if _, err := s.DB().Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	got, err := s2.LoadMemoryEmbeddings(ctx, "e5", []string{"h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("a code-graph migration wiped the memory vectors; re-embedding costs real API calls")
	}
}

// Prune is scoped: sweeping the workspace scope must not touch a session's
// vectors, and sweeping one session must not touch another's.
func TestMemoryEmbeddings_PruneIsScoped(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{
		{Hash: "ws-live", Scope: "ws", Vector: []float32{1, 0}},
		{Hash: "ws-dead", Scope: "ws", Vector: []float32{1, 0}},
		{Hash: "a1", Scope: "session:a", Vector: []float32{0, 1}},
		{Hash: "b1", Scope: "session:b", Vector: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	// Session A searches: it enumerated the workspace layers and its own
	// session, so those are the only scopes it may sweep.
	if err := s.PruneMemoryEmbeddings(ctx, "e5", []string{"ws", "session:a"}, []string{"ws-live", "a1"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"ws-live", "ws-dead", "a1", "b1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["b1"]; !ok {
		t.Error("session A's prune evicted session B's vector; both sessions would thrash re-embedding")
	}
	if _, ok := got["a1"]; !ok {
		t.Error("session A's own live vector was evicted")
	}
	if _, ok := got["ws-live"]; !ok {
		t.Error("a live workspace vector was evicted")
	}
	if _, ok := got["ws-dead"]; ok {
		t.Error("a dead workspace vector survived; the prune did nothing")
	}
}

// An empty keep set clears only the named scopes, not the whole model.
func TestMemoryEmbeddings_PruneWithEmptyKeepStaysInScope(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{
		{Hash: "a1", Scope: "session:a", Vector: []float32{0, 1}},
		{Hash: "b1", Scope: "session:b", Vector: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneMemoryEmbeddings(ctx, "e5", []string{"session:a"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"a1", "b1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["a1"]; ok {
		t.Error("session A's scope was not cleared")
	}
	if _, ok := got["b1"]; !ok {
		t.Error("clearing one scope wiped another")
	}
}

// No scopes means nothing to sweep — never "sweep everything".
func TestMemoryEmbeddings_PruneWithNoScopesIsANoop(t *testing.T) {
	s, _ := memStore(t)
	ctx := context.Background()

	if err := s.SaveMemoryEmbeddings(ctx, "e5", []MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneMemoryEmbeddings(ctx, "e5", nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemoryEmbeddings(ctx, "e5", []string{"h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("an empty scope list deleted rows")
	}
}

// A table written before scopes existed cannot be pruned correctly. It is a
// cache, so it is dropped and rebuilt rather than migrated into a shape whose
// rows all claim the same empty scope.
func TestMemoryEmbeddings_PreScopeTableIsRebuilt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ckg.db")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-scope shape.
	if _, err := s.DB().Exec(`DROP TABLE memory_embeddings`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`CREATE TABLE memory_embeddings (
        chunk_hash TEXT NOT NULL, model TEXT NOT NULL, dim INTEGER NOT NULL,
        vector BLOB NOT NULL, PRIMARY KEY (chunk_hash, model))`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO memory_embeddings VALUES ('old','e5',2,x'0000')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("opening a database with a pre-scope table failed: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	if err := s2.SaveMemoryEmbeddings(context.Background(), "e5",
		[]MemoryVector{{Hash: "h1", Scope: "ws", Vector: []float32{1, 0}}}); err != nil {
		t.Fatalf("the rebuilt table does not accept scoped rows: %v", err)
	}
}

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
	if err := s.SaveMemoryEmbeddings(ctx, "e5", want); err != nil {
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

	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{"h1": {1, 0}}); err != nil {
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

	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{"h1": {1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{"h1": {0, 1}}); err != nil {
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

	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{
		"keep": {1, 0}, "gone": {0, 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryEmbeddings(ctx, "other", map[string][]float32{"gone": {1, 1}}); err != nil {
		t.Fatal(err)
	}

	if err := s.PruneMemoryEmbeddings(ctx, "e5", []string{"keep"}); err != nil {
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

	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{"h1": {1, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneMemoryEmbeddings(ctx, "e5", nil); err != nil {
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

	if err := s.SaveMemoryEmbeddings(ctx, "e5", map[string][]float32{"h1": {1, 0}}); err != nil {
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

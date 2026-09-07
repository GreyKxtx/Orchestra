package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// countingEmbedder returns a deterministic vector per input and records every
// text it was asked to embed, so a test can assert what was *not* re-sent.
type countingEmbedder struct {
	mu    sync.Mutex
	calls int
	seen  []string
	model string
	fail  error
}

func (e *countingEmbedder) Model() string {
	if e.model == "" {
		return "test-embed"
	}
	return e.model
}

func (e *countingEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	e.seen = append(e.seen, inputs...)
	e.mu.Unlock()
	if e.fail != nil {
		return nil, e.fail
	}
	out := make([][]float32, len(inputs))
	for i, in := range inputs {
		out[i] = toyVector(in)
	}
	return out, nil
}

func (e *countingEmbedder) embedded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.seen...)
}

// toyVector maps text to a 3-d vector by keyword, so similarity is predictable
// without a real embedding server.
func toyVector(s string) []float32 {
	s = strings.ToLower(s)
	var v []float32
	switch {
	case strings.Contains(s, "postgres"):
		v = []float32{1, 0, 0}
	case strings.Contains(s, "docker"):
		v = []float32{0, 1, 0}
	default:
		v = []float32{0, 0, 1}
	}
	return v
}

// memVectorStore is an in-process VectorStore, standing in for the CKG table.
type memVectorStore struct {
	mu    sync.Mutex
	rows  map[string]map[string][]float32 // model -> hash -> vector
	scope map[string]map[string]string    // model -> hash -> scope
}

func newMemVectorStore() *memVectorStore {
	return &memVectorStore{
		rows:  map[string]map[string][]float32{},
		scope: map[string]map[string]string{},
	}
}

func (m *memVectorStore) Load(_ context.Context, model string, hashes []string) (map[string][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]float32{}
	for _, h := range hashes {
		if v, ok := m.rows[model][h]; ok {
			out[h] = v
		}
	}
	return out, nil
}

func (m *memVectorStore) Save(_ context.Context, model string, vecs []ScopedVector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows[model] == nil {
		m.rows[model] = map[string][]float32{}
		m.scope[model] = map[string]string{}
	}
	for _, v := range vecs {
		m.rows[model][v.Hash] = v.Vector
		m.scope[model][v.Hash] = v.Scope
	}
	return nil
}

func (m *memVectorStore) Prune(_ context.Context, model string, scopes []string, keep []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inScope := map[string]bool{}
	for _, sc := range scopes {
		inScope[sc] = true
	}
	live := map[string]bool{}
	for _, h := range keep {
		live[h] = true
	}
	for h := range m.rows[model] {
		if inScope[m.scope[model][h]] && !live[h] {
			delete(m.rows[model], h)
			delete(m.scope[model], h)
		}
	}
	return nil
}

func (m *memVectorStore) count(model string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows[model])
}

// memoryRoot builds a store whose repo layer holds exactly these entries.
func memoryRoot(t *testing.T, entries ...string) (string, *Store) {
	t.Helper()
	root := t.TempDir()
	cfg := Config{}
	cfg.Normalize()
	s := NewStore(root, "sess-1", cfg)
	writeRepoMemory(t, s, entries...)
	return root, s
}

// writeRepoMemory writes the agent file directly, in its on-disk entry format.
//
// Deliberately not through AppendTyped: writes are deduplicated, so near-identical
// filler entries merge into one and a test that wants sixty chunks silently gets
// two. Dedup is a write-path feature with its own tests; what matters here is the
// read path, which is exercised in full.
func writeRepoMemory(t *testing.T, s *Store, entries ...string) {
	t.Helper()
	path := filepath.Join(s.workspaceRoot, ".orchestra", "memory", "agent.md")
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
}

// The old search truncated to the last 48 chunks, so anything older was
// invisible to semantic ranking no matter how well it matched. With an index
// there is no reason to drop the tail.
func TestSemanticSearch_RanksBeyondTheOldForty8ChunkWindow(t *testing.T) {
	// collectSearchChunks yields the agent file's entries in reverse write
	// order, and the old window kept the LAST 48 of them — so the chunks it
	// dropped are the ones written last. The entry the query is looking for
	// goes there, or the test would pass with the window still in place.
	entries := make([]string, 0, 60)
	for i := 0; i < 59; i++ {
		entries = append(entries, fmt.Sprintf("unrelated note number %d about nothing", i))
	}
	entries = append(entries, "the database is postgres 16 with pgbouncer in front")
	root, store := memoryRoot(t, entries...)

	emb := &countingEmbedder{}
	vs := newMemVectorStore()
	hits, err := SemanticSearchIndexed(context.Background(), store, root, "which postgres version", 3, emb, vs)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits at all")
	}
	if !strings.Contains(hits[0].Snippet, "postgres") {
		t.Fatalf("top hit is %q; the matching entry is 1 of 60 and used to fall outside the last-48 window", hits[0].Snippet)
	}
}

// The point of the index: a second search re-embeds the query and nothing else.
func TestSemanticSearch_SecondSearchEmbedsOnlyTheQuery(t *testing.T) {
	root, store := memoryRoot(t,
		"the database is postgres 16",
		"builds run in docker compose",
		"the office cat is called Mila")

	vs := newMemVectorStore()
	first := &countingEmbedder{}
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "database?", 3, first, vs); err != nil {
		t.Fatal(err)
	}
	if got := len(first.embedded()); got != 4 {
		t.Fatalf("first search embedded %d texts, want 4 (query + 3 chunks)", got)
	}

	second := &countingEmbedder{}
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "database?", 3, second, vs); err != nil {
		t.Fatal(err)
	}
	got := second.embedded()
	if len(got) != 1 || got[0] != "database?" {
		t.Fatalf("second search embedded %v; only the query should be new", got)
	}
}

// An edited entry has a new content hash, so exactly that one is re-embedded
// and the untouched ones are not.
func TestSemanticSearch_ReEmbedsOnlyTheChangedChunk(t *testing.T) {
	root, store := memoryRoot(t,
		"the database is postgres 16",
		"builds run in docker compose")

	vs := newMemVectorStore()
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}

	writeRepoMemory(t, store, "the database is postgres 17", "builds run in docker compose")

	emb := &countingEmbedder{}
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, emb, vs); err != nil {
		t.Fatal(err)
	}
	got := emb.embedded()
	if len(got) != 2 {
		t.Fatalf("embedded %v, want the query and the one edited entry", got)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "postgres 17") {
		t.Errorf("the edited entry was not re-embedded: %v", got)
	}
	if strings.Contains(joined, "docker") {
		t.Errorf("an unchanged entry was re-embedded: %v", got)
	}
}

// The vector for text that no longer exists is dead weight and must not
// accumulate across edits.
func TestSemanticSearch_PrunesVectorsForDeletedEntries(t *testing.T) {
	root, store := memoryRoot(t,
		"the database is postgres 16",
		"builds run in docker compose")

	vs := newMemVectorStore()
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	if n := vs.count("test-embed"); n != 2 {
		t.Fatalf("stored %d vectors, want 2", n)
	}

	writeRepoMemory(t, store, "the database is postgres 16")
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, &countingEmbedder{}, vs); err != nil {
		t.Fatal(err)
	}
	if n := vs.count("test-embed"); n != 1 {
		t.Fatalf("stored %d vectors after deleting an entry, want 1", n)
	}
}

// A failing embedding endpoint must surface as an error so memory_search can
// say "degraded" — silently returning nothing would look like "no matches".
func TestSemanticSearch_EmbedFailureIsReported(t *testing.T) {
	root, store := memoryRoot(t, "the database is postgres 16")
	emb := &countingEmbedder{fail: fmt.Errorf("connection refused")}
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, emb, newMemVectorStore()); err == nil {
		t.Fatal("a dead embedding endpoint returned no error; the caller cannot report it as degraded")
	}
}

// A nil store is the no-index path: it must still work, just without caching.
func TestSemanticSearch_WorksWithoutAVectorStore(t *testing.T) {
	root, store := memoryRoot(t, "the database is postgres 16", "the office cat is called Mila")
	hits, err := SemanticSearchIndexed(context.Background(), store, root, "postgres", 3, &countingEmbedder{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Snippet, "postgres") {
		t.Fatalf("hits = %+v", hits)
	}
}

// Vectors are stored per model, so switching embed.model must not rank the
// query against the previous model's vectors.
func TestSemanticSearch_ModelSwitchReEmbeds(t *testing.T) {
	root, store := memoryRoot(t, "the database is postgres 16", "builds run in docker compose")
	vs := newMemVectorStore()

	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, &countingEmbedder{model: "m1"}, vs); err != nil {
		t.Fatal(err)
	}
	emb2 := &countingEmbedder{model: "m2"}
	if _, err := SemanticSearchIndexed(context.Background(), store, root, "db", 3, emb2, vs); err != nil {
		t.Fatal(err)
	}
	if got := len(emb2.embedded()); got != 3 {
		t.Fatalf("model switch embedded %d texts, want 3 (query + both chunks re-embedded under the new model)", got)
	}
}

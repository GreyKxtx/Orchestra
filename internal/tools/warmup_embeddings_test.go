package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

func fakeEmbedBase(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": []float32{float32(i + 1), 0.5, 0.25}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func newWarmupRunner(t *testing.T, emb config.EmbedConfig) (*Runner, *ckg.Store) {
	t.Helper()
	root := t.TempDir()
	src := "package pkg\n\ntype Agent struct{}\n\nfunc (a *Agent) Run() error {\n\treturn nil\n}\n"
	if err := os.WriteFile(filepath.Join(root, "agent.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(root, RunnerOptions{Embed: emb})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	r.ckgMu.RLock()
	store := r.ckgStore
	r.ckgMu.RUnlock()
	if store == nil {
		t.Fatal("runner has no CKG store")
	}
	nodes := []ckg.Node{
		{FQN: "pkg.Agent", ShortName: "Agent", Kind: "struct", LineStart: 3, LineEnd: 3},
		{FQN: "pkg.Agent.Run", ShortName: "Agent.Run", Kind: "method", LineStart: 5, LineEnd: 7},
	}
	if err := store.SaveFileNodes(context.Background(), "agent.go", "h1", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}
	return r, store
}

func closedGraphUpdate() <-chan error {
	ch := make(chan error, 1)
	ch <- nil
	close(ch)
	return ch
}

func TestWarmupEmbeddings_IndexesGraphNodes(t *testing.T) {
	r, store := newWarmupRunner(t, config.EmbedConfig{
		Model: "nomic-embed-text", APIBase: fakeEmbedBase(t), TimeoutS: 5,
	})

	select {
	case <-r.WarmupEmbeddings(context.Background(), closedGraphUpdate()):
	case <-time.After(20 * time.Second):
		t.Fatal("warmup never finished")
	}

	n, err := store.CountEmbeddings(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("semantic_search needs an index it did not have to be asked for; got %d embeddings", n)
	}
}

func TestWarmupEmbeddings_WaitsForTheGraph(t *testing.T) {
	r, store := newWarmupRunner(t, config.EmbedConfig{
		Model: "nomic-embed-text", APIBase: fakeEmbedBase(t), TimeoutS: 5,
	})
	graph := make(chan error, 1)

	done := r.WarmupEmbeddings(context.Background(), graph)

	// Nothing may be embedded while the graph scan is still running — there
	// would be no nodes yet, and the pass would silently index nothing.
	select {
	case <-done:
		t.Fatal("warmup ran before the graph update finished")
	case <-time.After(150 * time.Millisecond):
	}
	graph <- nil
	close(graph)

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("warmup never finished")
	}
	if n, _ := store.CountEmbeddings(context.Background(), "nomic-embed-text"); n != 2 {
		t.Fatalf("got %d embeddings after the graph settled", n)
	}
}

func TestWarmupEmbeddings_NoModelIsANoOp(t *testing.T) {
	r, store := newWarmupRunner(t, config.EmbedConfig{})

	select {
	case <-r.WarmupEmbeddings(context.Background(), closedGraphUpdate()):
	case <-time.After(5 * time.Second):
		t.Fatal("a disabled warmup must return at once")
	}

	if n, _ := store.CountEmbeddings(context.Background(), ""); n != 0 {
		t.Fatalf("nothing to index without a model, got %d", n)
	}
}

func TestWarmupEmbeddings_RespectsExplicitOptOut(t *testing.T) {
	off := false
	r, store := newWarmupRunner(t, config.EmbedConfig{
		Model: "nomic-embed-text", APIBase: fakeEmbedBase(t), TimeoutS: 5, AutoIndex: &off,
	})

	select {
	case <-r.WarmupEmbeddings(context.Background(), closedGraphUpdate()):
	case <-time.After(5 * time.Second):
		t.Fatal("a disabled warmup must return at once")
	}

	// Paid endpoints charge per call; opting out has to actually stop the call.
	if n, _ := store.CountEmbeddings(context.Background(), "nomic-embed-text"); n != 0 {
		t.Fatalf("embed.auto_index: false still indexed %d node(s)", n)
	}
}

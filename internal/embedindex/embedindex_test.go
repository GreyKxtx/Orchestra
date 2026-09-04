package embedindex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

// fakeEmbedServer answers the OpenAI-compatible embeddings contract with one
// vector per input, so batching bugs surface as a length mismatch.
func fakeEmbedServer(t *testing.T, calls *int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if calls != nil {
			*calls++
		}
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": []float32{float32(i + 1), 0.5, 0.25}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func seedWorkspace(t *testing.T, calls *int) (root string, store *ckg.Store, emb config.EmbedConfig) {
	t.Helper()
	root = t.TempDir()
	src := "package pkg\n\ntype Agent struct{}\n\nfunc (a *Agent) Run() error {\n\treturn nil\n}\n"
	if err := os.WriteFile(filepath.Join(root, "agent.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := ckg.NewStore(filepath.Join(root, "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	nodes := []ckg.Node{
		{FQN: "pkg.Agent", ShortName: "Agent", Kind: "struct", LineStart: 3, LineEnd: 3},
		{FQN: "pkg.Agent.Run", ShortName: "Agent.Run", Kind: "method", LineStart: 5, LineEnd: 7},
	}
	if err := store.SaveFileNodes(context.Background(), "agent.go", "h1", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}
	emb = config.EmbedConfig{
		Model:    "nomic-embed-text",
		APIBase:  fakeEmbedServer(t, calls),
		TimeoutS: 5,
	}
	return root, store, emb
}

func TestRun_IndexesEveryMissingNode(t *testing.T) {
	root, store, emb := seedWorkspace(t, nil)

	res, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb})

	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 2 {
		t.Errorf("Indexed = %d, want 2 (%+v)", res.Indexed, res)
	}
	n, _ := store.CountEmbeddings(context.Background(), emb.Model)
	if n != 2 {
		t.Errorf("persisted embeddings = %d, want 2", n)
	}
}

func TestRun_IsIncremental(t *testing.T) {
	root, store, emb := seedWorkspace(t, nil)
	if _, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb}); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb})

	if err != nil {
		t.Fatal(err)
	}
	// Warmup runs this on every core start. Re-embedding an unchanged repo
	// would spend real money on a paid endpoint each time.
	if res.Indexed != 0 || res.Total != 0 {
		t.Fatalf("second run must embed nothing, got %+v", res)
	}
}

func TestRun_RebuildReplacesExistingVectors(t *testing.T) {
	root, store, emb := seedWorkspace(t, nil)
	if _, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb}); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb, Rebuild: true})

	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 2 {
		t.Fatalf("rebuild must re-embed everything, got %+v", res)
	}
}

func TestRun_BatchesRequests(t *testing.T) {
	calls := 0
	root, store, emb := seedWorkspace(t, &calls)
	emb.BatchSize = 1

	if _, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb}); err != nil {
		t.Fatal(err)
	}

	if calls != 2 {
		t.Fatalf("batch_size 1 over 2 nodes must make 2 requests, got %d", calls)
	}
}

func TestRun_RefusesWithoutModel(t *testing.T) {
	root, store, emb := seedWorkspace(t, nil)
	emb.Model = ""

	_, err := Run(context.Background(), Options{ProjectRoot: root, Store: store, Embed: emb})

	if err == nil {
		t.Fatal("indexing without an embedding model must be an error, not a silent no-op")
	}
	if !strings.Contains(err.Error(), "embed.model") {
		t.Errorf("error must name the missing key, got: %v", err)
	}
}

func TestRun_ReportsProgress(t *testing.T) {
	root, store, emb := seedWorkspace(t, nil)
	var seen int

	_, err := Run(context.Background(), Options{
		ProjectRoot: root, Store: store, Embed: emb,
		Progress: func(done, total int, _ string) {
			seen = done
			if total != 2 {
				t.Errorf("total = %d, want 2", total)
			}
		},
	})

	if err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("progress must reach the total, last done = %d", seen)
	}
}

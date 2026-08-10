package nav

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

func fakeEmbedServer(t *testing.T, vec []float32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		data := make([]struct {
			Embedding []float32 `json:"embedding"`
		}, len(req.Input))
		for i := range req.Input {
			data[i].Embedding = vec
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestSemanticSearch_HappyPath(t *testing.T) {
	srv := fakeEmbedServer(t, []float32{1, 0, 0})
	defer srv.Close()

	root := t.TempDir()
	store, err := ckg.NewStore(filepath.Join(root, "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodes := []ckg.Node{
		{FQN: "pkg.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 1},
		{FQN: "pkg.Bar", ShortName: "Bar", Kind: "func", LineStart: 2, LineEnd: 2},
	}
	if err := store.SaveFileNodes(context.Background(), "x.go", "h", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}
	var idFoo, idBar int64
	rows, _ := store.DB().Query(`SELECT id, fqn FROM nodes`)
	for rows.Next() {
		var id int64
		var fqn string
		_ = rows.Scan(&id, &fqn)
		if fqn == "pkg.Foo" {
			idFoo = id
		} else {
			idBar = id
		}
	}
	rows.Close()

	_ = store.SaveEmbeddings(context.Background(), "test-model", []ckg.EmbeddingItem{
		{NodeID: idFoo, Vector: []float32{1, 0, 0}},
		{NodeID: idBar, Vector: []float32{0, 1, 0}},
	})

	snap := CKGAccess{Store: store}
	c := NewClient(root, nil, config.EmbedConfig{APIBase: srv.URL + "/v1", Model: "test-model"}, func() (CKGAccess, func()) {
		return snap, func() {}
	}, nil)

	resp, err := c.SemanticSearch(context.Background(), SemanticSearchRequest{Query: "foo function", TopK: 2})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if resp.Model != "test-model" {
		t.Errorf("model: %q", resp.Model)
	}
	if len(resp.Hits) != 2 {
		t.Fatalf("hits: %d", len(resp.Hits))
	}
	if resp.Hits[0].FQN != "pkg.Foo" {
		t.Errorf("top hit: %q (want pkg.Foo)", resp.Hits[0].FQN)
	}
}

func TestSemanticSearch_NoEmbedModel(t *testing.T) {
	c := NewClient(t.TempDir(), nil, config.EmbedConfig{}, nil, nil)
	_, err := c.SemanticSearch(context.Background(), SemanticSearchRequest{Query: "x"})
	if err == nil || !strings.Contains(err.Error(), "embed.model") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestSemanticSearch_NoCKG(t *testing.T) {
	c := NewClient(t.TempDir(), nil, config.EmbedConfig{Model: "m"}, nil, nil)
	_, err := c.SemanticSearch(context.Background(), SemanticSearchRequest{Query: "x"})
	if err == nil || !strings.Contains(err.Error(), "CKG") {
		t.Fatalf("expected no-CKG error, got %v", err)
	}
}

func TestSemanticSearch_EmptyQuery(t *testing.T) {
	store, _ := ckg.NewStore(":memory:")
	defer store.Close()
	snap := CKGAccess{Store: store}
	c := NewClient(t.TempDir(), nil, config.EmbedConfig{Model: "m"}, func() (CKGAccess, func()) {
		return snap, func() {}
	}, nil)
	_, err := c.SemanticSearch(context.Background(), SemanticSearchRequest{Query: "  "})
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected empty-query error, got %v", err)
	}
}

package nav

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
)

// fakeEmbedEndpoint answers the OpenAI-compatible embeddings contract so the
// test exercises an empty index rather than a broken endpoint.
func fakeEmbedEndpoint(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1, 0.2, 0.3}}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func newEmptyIndexClient(t *testing.T) *Client {
	t.Helper()
	root := t.TempDir()
	store, err := ckg.NewStore(filepath.Join(root, "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return NewClient(root, nil,
		config.EmbedConfig{Model: "nomic-embed-text", APIBase: fakeEmbedEndpoint(t), TimeoutS: 5},
		func() (CKGAccess, func()) { return CKGAccess{Store: store}, func() {} },
		nil,
	)
}

func TestSemanticSearch_EmptyIndexExplainsHowToBuildIt(t *testing.T) {
	c := newEmptyIndexClient(t)

	res, err := c.SemanticSearch(context.Background(), SemanticSearchRequest{
		Query: "where is the search panel rendered",
	})

	if err != nil {
		t.Fatalf("an unbuilt index is a setup gap, not a tool error: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("nothing is indexed, so there can be no hits: %+v", res.Hits)
	}
	// Zero hits with no explanation reads exactly like "nothing matches your
	// query", so the index never gets built and the tool looks permanently
	// broken. Embeddings are only ever written by an explicit command.
	if !strings.Contains(res.NextStep, "ckg embed") {
		t.Fatalf("an empty index must name the command that fills it, got NextStep=%q", res.NextStep)
	}
}

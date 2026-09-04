package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
)

func newMemoryClient(t *testing.T, embedCfg config.EmbedConfig) (*Client, string) {
	t.Helper()
	root := t.TempDir()
	memDir := filepath.Join(root, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "agent.md"),
		[]byte("\n---\n*2026-01-01T00:00:00Z*\n\nvite is the bundler for this project\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewClient(root,
		func() string { return "" },
		func() memory.Config { return memory.DefaultConfig() },
		func() config.EmbedConfig { return embedCfg },
		func() *ckg.Store { return nil },
	)
	return c, root
}

func TestMemorySearch_ReportsWhenSemanticRankingIsUnavailable(t *testing.T) {
	// embed.model is set, so the model is told memory_search ranks semantically —
	// but the endpoint refuses. Port 9 is the discard port: nothing listens.
	c, _ := newMemoryClient(t, config.EmbedConfig{
		Model:    "nomic-embed-text",
		APIBase:  "http://127.0.0.1:9/v1",
		TimeoutS: 1,
	})

	res, err := c.MemorySearch(context.Background(), MemorySearchRequest{Query: "vite"})

	if err != nil {
		t.Fatalf("a broken embedding endpoint must not fail the search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("the substring fallback must still answer")
	}
	// Silently returning substring results under a semantic contract is how a
	// misconfigured embedding endpoint stays invisible for a whole field run.
	if strings.TrimSpace(res.Degraded) == "" {
		t.Fatal("falling back to substring must be reported, not swallowed")
	}
}

func TestMemorySearch_QuietWhenEmbedNotConfigured(t *testing.T) {
	c, _ := newMemoryClient(t, config.EmbedConfig{})

	res, err := c.MemorySearch(context.Background(), MemorySearchRequest{Query: "vite"})

	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("substring search must work without embeddings")
	}
	// Nothing was promised, so nothing was lost: no warning to spend tokens on.
	if res.Degraded != "" {
		t.Fatalf("unconfigured embed is not a degradation, got %q", res.Degraded)
	}
}

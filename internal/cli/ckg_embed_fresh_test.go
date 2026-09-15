package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `orchestra ckg embed` is what the semantic_search description tells the user
// to run. On a project nothing had run in yet it did two wrong things in a row,
// both seen against a real local embedding model:
//
//  1. .orchestra/ did not exist, so the store failed with a bare
//     "open ckg store: unable to open database file (14)";
//  2. with the directory present, the graph had never been built — only core's
//     warmup builds it — so it answered "No nodes need embedding", which reads
//     as "the index is complete" while semantic_search finds nothing.
//
// A fake embeddings endpoint stands in for the model: what is under test is the
// command, not the vectors.
func TestCKGEmbed_OnAFreshProjectBuildsTheGraphAndIndexesIt(t *testing.T) {
	var embedded int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		embedded += len(req.Input)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{0.1, 0.2, 0.3}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": "fake-embed"})
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	files := map[string]string{
		"go.mod":   "module freshws\n\ngo 1.21\n",
		"retry.go": "package freshws\n\n// Retry runs fn until it succeeds.\nfunc Retry(fn func() error) error { return fn() }\n\ntype Policy struct{ Attempts int }\n",
		".orchestra.yml": "project_root: .\nllm:\n  api_base: " + srv.URL + "/v1\n  model: unused\n" +
			"embed:\n  api_base: " + srv.URL + "/v1\n  model: fake-embed\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".orchestra")); err == nil {
		t.Fatal("fixture already has .orchestra/, so this is not a fresh project")
	}
	t.Chdir(root)

	var out bytes.Buffer
	ckgEmbedCmd.SetOut(&out)
	ckgEmbedCmd.SetErr(&out)
	t.Cleanup(func() { ckgEmbedCmd.SetOut(nil); ckgEmbedCmd.SetErr(nil) })
	ckgEmbedRebuild, ckgEmbedLimit, ckgEmbedBatchSize = false, 0, 0
	// Called directly rather than through cobra's Execute, so no context is set.
	// A nil context makes database/sql panic while holding its mutex, and the
	// deferred store.Close then waits on that mutex forever: the first run of
	// this test hung for ten minutes instead of failing.
	ckgEmbedCmd.SetContext(context.Background())

	if err := runCKGEmbed(ckgEmbedCmd, nil); err != nil {
		t.Fatalf("ckg embed failed on a fresh project: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "No nodes need embedding") {
		t.Fatalf("a project whose graph was never built was reported as fully indexed:\n%s", out.String())
	}
	if embedded == 0 {
		t.Fatalf("nothing was sent to the embedding model:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Done.") {
		t.Errorf("no completion line:\n%s", out.String())
	}
}

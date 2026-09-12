package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newGraphTestCore(t *testing.T) *Core {
	t.Helper()
	root := t.TempDir()
	body := "project_root: .\nllm:\n  api_base: http://127.0.0.1:1/v1\n  model: m\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// index.graph at file level: the two seeded files, the folder they share,
// and each file hanging off it. Symbol level shows the symbols themselves.
func TestIndexGraph_FileAndSymbolLevels(t *testing.T) {
	c := newGraphTestCore(t)
	ctx := context.Background()
	if err := c.tools.SeedCKGSymbolForTest(ctx, "pkg/a.go", "h1", "A", 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.tools.SeedCKGSymbolForTest(ctx, "pkg/b.go", "h2", "B", 1, 3); err != nil {
		t.Fatal(err)
	}

	files, err := c.IndexGraph(ctx, IndexGraphParams{})
	if err != nil {
		t.Fatal(err)
	}
	if !files.Available || files.Level != "file" {
		t.Fatalf("available=%v level=%q", files.Available, files.Level)
	}
	ids := map[string]string{}
	for _, n := range files.Nodes {
		ids[n.ID] = n.Group
	}
	if ids["pkg/a.go"] != "file" || ids["pkg/b.go"] != "file" || ids["pkg"] != "folder" {
		t.Fatalf("file-level nodes = %v", ids)
	}
	inFolder := 0
	for _, l := range files.Links {
		if l.Relation == "in_folder" && l.Target == "pkg" {
			inFolder++
		}
		if l.Relation == "in_file" {
			t.Fatalf("symbol-level link in the file graph: %+v", l)
		}
	}
	if inFolder != 2 {
		t.Fatalf("in_folder links = %d, want 2: %v", inFolder, files.Links)
	}

	symbols, err := c.IndexGraph(ctx, IndexGraphParams{Level: "symbol"})
	if err != nil {
		t.Fatal(err)
	}
	seenSymbol := false
	for _, n := range symbols.Nodes {
		if n.Group == "func" && (n.Name == "A" || n.Name == "B") {
			seenSymbol = true
		}
	}
	if !seenSymbol {
		t.Fatalf("symbol-level graph has no symbol nodes: %v", symbols.Nodes)
	}

	if _, err := c.IndexGraph(ctx, IndexGraphParams{Level: "galaxy"}); err == nil {
		t.Fatal("an unknown level must be refused")
	}
}

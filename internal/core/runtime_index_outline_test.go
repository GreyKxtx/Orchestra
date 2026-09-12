package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Graph view reads a file's functions and the first lines of each.
func TestIndexOutline_ListsSymbolsWithTheirSource(t *testing.T) {
	c := newGraphTestCore(t)
	ctx := context.Background()
	// One symbol per file: seeding replaces a file's nodes.
	if err := c.tools.SeedCKGSymbolForTest(ctx, "pkg/a.go", "h1", "Alpha", 2, 4); err != nil {
		t.Fatal(err)
	}
	if err := c.tools.SeedCKGSymbolForTest(ctx, "pkg/b.go", "h2", "Beta", 6, 80); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0, 90)
	for i := 1; i <= 90; i++ {
		lines = append(lines, "line "+string(rune('0'+i%10)))
	}
	lines[1] = "func Alpha() {"
	lines[5] = "func Beta() {"
	if err := os.MkdirAll(filepath.Join(c.workspaceRoot, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(strings.Join(lines, "\n"))
	if err := os.WriteFile(filepath.Join(c.workspaceRoot, "pkg", "a.go"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.workspaceRoot, "pkg", "b.go"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Available {
		t.Fatalf("the seeded file must be in the graph: %+v", out)
	}
	if out.Lines != 90 || out.Bytes != len(body) {
		t.Fatalf("lines = %d bytes = %d", out.Lines, out.Bytes)
	}
	if len(out.Symbols) != 1 || out.Symbols[0].Name != "Alpha" {
		t.Fatalf("symbols = %+v", out.Symbols)
	}
	// The preview starts at the symbol's first line and stops at its last.
	if !strings.HasPrefix(out.Symbols[0].Preview, "func Alpha() {") || out.Symbols[0].Truncated {
		t.Fatalf("Alpha preview = %q truncated=%v", out.Symbols[0].Preview, out.Symbols[0].Truncated)
	}
	if strings.Count(out.Symbols[0].Preview, "\n") != 2 {
		t.Fatalf("Alpha preview must be its three lines: %q", out.Symbols[0].Preview)
	}

	// Beta runs 75 lines; the preview stops at the cap and says so.
	long, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "pkg/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(long.Symbols) != 1 {
		t.Fatalf("symbols = %+v", long.Symbols)
	}
	if !strings.HasPrefix(long.Symbols[0].Preview, "func Beta() {") || !long.Symbols[0].Truncated {
		t.Fatalf("Beta preview = %q truncated=%v", long.Symbols[0].Preview, long.Symbols[0].Truncated)
	}
	if got := strings.Count(long.Symbols[0].Preview, "\n") + 1; got != outlinePreviewLines {
		t.Fatalf("Beta preview lines = %d, want %d", got, outlinePreviewLines)
	}

	// preview:false is the list alone.
	no := false
	bare, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "pkg/a.go", Preview: &no})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range bare.Symbols {
		if s.Preview != "" {
			t.Fatalf("preview:false must read nothing: %+v", s)
		}
	}
}

func TestIndexOutline_UnknownFileAndBadPath(t *testing.T) {
	c := newGraphTestCore(t)
	ctx := context.Background()

	out, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "pkg/missing.go"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Available || len(out.Symbols) != 0 {
		t.Fatalf("a file that is not indexed is an empty answer: %+v", out)
	}

	if _, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "  "}); err == nil {
		t.Fatal("an empty path must be refused")
	}
	if _, err := c.IndexOutline(ctx, IndexOutlineParams{Path: "../outside.go"}); err == nil {
		t.Fatal("a path outside the workspace must be refused")
	}
}

// The picture and its counters arrive together: the Graph view shows both.
func TestIndexGraph_CarriesTheIndexCounters(t *testing.T) {
	c := newGraphTestCore(t)
	ctx := context.Background()
	if err := c.tools.SeedCKGSymbolForTest(ctx, "pkg/a.go", "h1", "A", 1, 3); err != nil {
		t.Fatal(err)
	}
	g, err := c.IndexGraph(ctx, IndexGraphParams{})
	if err != nil {
		t.Fatal(err)
	}
	if !g.Stats.Available || g.Stats.Files < 1 || g.Stats.Nodes < 1 {
		t.Fatalf("stats = %+v", g.Stats)
	}
	if g.Stats.DBPath != "" {
		t.Fatalf("the graph answer must not carry a filesystem path: %q", g.Stats.DBPath)
	}
}

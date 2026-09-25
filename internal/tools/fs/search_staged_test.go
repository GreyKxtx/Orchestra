package fs

import (
	"context"
	"testing"
)

func grepPaths(t *testing.T, c *Client, query string, scope ...string) map[string]int {
	t.Helper()
	res, err := c.SearchText(context.Background(), SearchTextRequest{Query: query, Paths: scope})
	if err != nil {
		t.Fatalf("grep %q: %v", query, err)
	}
	got := map[string]int{}
	for _, m := range res.Matches {
		got[m.Path] = m.Line
	}
	return got
}

// LLM-11: in a dry run grep searched the disk while read and edit see the
// staged overlay. A line the turn had already changed was still found, a line
// it had added was not, and the edit built from the hit failed as stale.
func TestSearchText_SeesTheStagedOverlay(t *testing.T) {
	c, _ := workspaceWithFile(t, "a.go", "package a\n\nvar oldName = 1\n")
	ctx := context.Background()
	if _, err := c.Edit(ctx, FSEditRequest{Path: "a.go", Search: "var oldName = 1", Replace: "var newName = 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(ctx, FSWriteRequest{Path: "pkg/b.go", Content: "package pkg\n\nfunc use_newName() {}\n"}); err != nil {
		t.Fatal(err)
	}

	if got := grepPaths(t, c, "oldName"); len(got) != 0 {
		t.Fatalf("grep found a line the turn already changed: %v", got)
	}
	if got := grepPaths(t, c, "newName"); got["a.go"] != 3 || got["pkg/b.go"] != 3 {
		t.Fatalf("grep misses the staged lines: %v", got)
	}
	if got := grepPaths(t, c, "newName", "pkg"); len(got) != 1 || got["pkg/b.go"] == 0 {
		t.Fatalf("a scoped grep searches only its scope: %v", got)
	}
}

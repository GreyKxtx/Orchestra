package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The enclosing symbol of a match is a graph lookup. It is looked up for the
// matches grep returns, not for every match in the tree: a common identifier
// used to cost thousands of lookups to return 200 lines (DATA-6).
func TestSearchText_EnrichesOnlyTheMatchesItReturns(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("package p\n\n")
	for i := 0; i < 300; i++ {
		b.WriteString("var needle" + strings.Repeat("x", i%7) + " = 1 // needle\n")
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewClient(root, nil, nil)
	lookups := 0
	c.Hooks.SymbolFQNAtLine = func(ctx context.Context, relPath string, line int) string {
		lookups++
		return "p.sym"
	}
	resp, err := c.SearchText(context.Background(), SearchTextRequest{Query: "needle", MaxMatches: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Matches) != 5 {
		t.Fatalf("max_matches caps the answer: %d", len(resp.Matches))
	}
	if lookups != 5 {
		t.Fatalf("the symbol lookup ran %d times for 5 returned matches", lookups)
	}
	for _, m := range resp.Matches {
		if m.SymbolFQN != "p.sym" {
			t.Fatalf("a returned match is still enriched: %+v", m)
		}
	}
}

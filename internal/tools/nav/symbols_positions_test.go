package nav

import (
	"context"
	"testing"
)

// The tiers without LSP answer in the same 1-based numbering as read: for
// `type Item struct` on line 4, column 6 is where "Item" starts.
func TestSymbols_TiersWithoutLSPAreOneBased(t *testing.T) {
	src := []byte("package main\n\n// Item is a thing.\ntype Item struct {\n\tName string\n}\n")

	find := func(t *testing.T, tier string, syms []Symbol) Symbol {
		t.Helper()
		for _, s := range syms {
			if s.Name == "Item" {
				return s
			}
		}
		t.Fatalf("%s: no Item in %+v", tier, syms)
		return Symbol{}
	}

	if s := find(t, "regex", goSymbolsViaRegex(src)); s.StartLine != 4 || s.StartCol != 6 || s.EndLine != 4 || s.EndCol != 10 {
		t.Errorf("regex: Item at %d:%d-%d:%d, want 4:6-4:10", s.StartLine, s.StartCol, s.EndLine, s.EndCol)
	}

	syms, ok := goSymbolsViaTreeSitter(context.Background(), src)
	if !ok {
		t.Skip("tree-sitter is not built in (CGO disabled)")
	}
	// tree-sitter spans the whole type_spec: "Item struct {...}" from 4:6 to 6:2.
	if s := find(t, "tree-sitter", syms); s.StartLine != 4 || s.StartCol != 6 || s.EndLine != 6 || s.EndCol != 2 {
		t.Errorf("tree-sitter: Item at %d:%d-%d:%d, want 4:6-6:2", s.StartLine, s.StartCol, s.EndLine, s.EndCol)
	}
}

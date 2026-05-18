// Package astedit performs identifier-level refactors via tree-sitter, so
// substitutions skip strings, comments, and unrelated sub-identifiers that
// text search-replace would clobber. It supports every language CKG already
// parses (see ckg.SitterLanguageFor).
package astedit

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/orchestra/orchestra/internal/ckg"
)

// RenameResult summarises one rename pass over a single file's contents.
type RenameResult struct {
	// NewContent is the post-rename source. When Count == 0 this equals the input.
	NewContent []byte
	// Count is the number of identifier sites that were rewritten.
	Count int
	// Sites lists 1-based line numbers where a replacement occurred. Useful for
	// preview text and tests; capped at 100 entries to keep the wire size sane.
	Sites []int
	// Skipped lists 1-based line numbers where the old name appeared inside a
	// string, comment, or as part of a longer identifier and was deliberately
	// left untouched.
	Skipped []int
}

// identifierLikeNodes is the set of tree-sitter node types that count as
// "rename me" sites. Different grammars use different names for the same
// concept (identifier / field_identifier / type_identifier / etc.) so we
// match any node whose type ends in "identifier".
//
// We also skip nodes inside containers like "string", "comment", or
// "raw_string_literal" — these are detected by walking up the parent chain.
func isIdentifierNode(n *sitter.Node) bool {
	t := n.Type()
	return strings.HasSuffix(t, "identifier") || t == "shorthand_property_identifier"
}

// isInsideStringOrComment reports whether n is descended from a string/comment
// node. Tree-sitter encodes string interpolations specially (they contain
// expression nodes whose identifiers SHOULD be renameable), so we only skip
// when the immediate textual container is itself a literal.
func isInsideStringOrComment(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		t := p.Type()
		switch {
		case strings.Contains(t, "string_literal"),
			strings.Contains(t, "raw_string"),
			strings.HasSuffix(t, "comment"),
			t == "string", t == "comment", t == "line_comment", t == "block_comment":
			return true
		case strings.Contains(t, "interpolation") || strings.Contains(t, "interpolated"):
			// Interpolated expressions inside strings ARE renameable.
			return false
		}
	}
	return false
}

// RenameInFile parses path's content with tree-sitter and rewrites every
// identifier node whose textual content equals oldName. The walk skips:
//   - identifiers inside string literals or comments,
//   - matches that are only a substring of a longer identifier (impossible
//     here because we compare node text, not byte offsets),
//   - empty / blank oldName or newName values.
//
// Returns the rewritten source and a Result describing what changed. When the
// file's extension has no tree-sitter grammar, returns an error so the caller
// can decide whether to fall back to plain text edit.
func RenameInFile(_ context.Context, path string, src []byte, oldName, newName string) (*RenameResult, error) {
	if strings.TrimSpace(oldName) == "" {
		return nil, fmt.Errorf("astedit: old_name is empty")
	}
	if strings.TrimSpace(newName) == "" {
		return nil, fmt.Errorf("astedit: new_name is empty")
	}
	if oldName == newName {
		return &RenameResult{NewContent: src}, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	lang := ckg.SitterLanguageFor(ext)
	if lang == nil {
		return nil, fmt.Errorf("astedit: no tree-sitter grammar for %s", ext)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(nil, nil, src)
	if err != nil {
		return nil, fmt.Errorf("astedit: parse %s: %w", path, err)
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil {
		return nil, fmt.Errorf("astedit: empty parse tree for %s", path)
	}

	type hit struct{ start, end uint32 }
	var hits []hit
	var sites []int
	var skipped []int

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		// Recurse first so nested identifiers are seen.
		count := int(n.NamedChildCount())
		for i := 0; i < count; i++ {
			walk(n.NamedChild(i))
		}
		if !isIdentifierNode(n) {
			return
		}
		if string(src[n.StartByte():n.EndByte()]) != oldName {
			return
		}
		if isInsideStringOrComment(n) {
			skipped = append(skipped, int(n.StartPoint().Row)+1)
			return
		}
		hits = append(hits, hit{n.StartByte(), n.EndByte()})
		sites = append(sites, int(n.StartPoint().Row)+1)
	}
	walk(root)

	if len(hits) == 0 {
		return &RenameResult{NewContent: src, Skipped: dedupInts(skipped)}, nil
	}

	// Walk hits in source order, building the rewritten buffer.
	sort.Slice(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
	out := make([]byte, 0, len(src)+len(hits)*(len(newName)-len(oldName)))
	cur := uint32(0)
	for _, h := range hits {
		out = append(out, src[cur:h.start]...)
		out = append(out, newName...)
		cur = h.end
	}
	out = append(out, src[cur:]...)

	if len(sites) > 100 {
		sites = sites[:100]
	}
	return &RenameResult{
		NewContent: out,
		Count:      len(hits),
		Sites:      dedupInts(sites),
		Skipped:    dedupInts(skipped),
	}, nil
}

func dedupInts(in []int) []int {
	if len(in) <= 1 {
		return in
	}
	sort.Ints(in)
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

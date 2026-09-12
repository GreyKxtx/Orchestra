package ckg

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/orchestra/orchestra/protocol"
)

// ValidateSyntax parses content with tree-sitter and rejects files that contain
// ERROR or MISSING nodes. Returns nil when the extension has no CKG grammar
// (gate skipped) or when content is syntactically OK.
func ValidateSyntax(relPath string, content []byte) error {
	ext := strings.ToLower(filepath.Ext(relPath))
	lang := SitterLanguageFor(ext)
	if lang == nil || len(content) == 0 {
		return nil
	}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)

	tree := parser.Parse(nil, content)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return nil
	}
	problem := findSyntaxProblemNode(root)
	if problem == nil {
		return nil
	}

	line := int(problem.StartPoint().Row) + 1
	rel := filepath.ToSlash(strings.TrimSpace(relPath))
	msg := fmt.Sprintf("syntax error at line %d — fix your patch before staging", line)
	return protocol.NewError(protocol.SyntaxError, msg, map[string]any{
		"path": rel,
		"line": line,
		"node": problem.Type(),
	})
}

// findSyntaxProblemNode returns the first node tree-sitter could not parse.
//
// A missing token is NOT a node of type "MISSING" — tree-sitter flags it on
// the node while Type() reports the token that should have been there ("}"
// for an unclosed brace). Matching on the type name meant the entire
// missing-token half of this check never fired, so the gate passed every file
// whose only fault was something left out. IsMissing is the flag that asks
// the question properly.
//
// HasError short-circuits whole subtrees that parsed cleanly, which matters
// on large files: without it every node is visited even when nothing is wrong.
func findSyntaxProblemNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.IsMissing() || n.Type() == "ERROR" {
		return n
	}
	if !n.HasError() {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if found := findSyntaxProblemNode(n.Child(i)); found != nil {
			return found
		}
	}
	return nil
}

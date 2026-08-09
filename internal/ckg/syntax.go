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

func findSyntaxProblemNode(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "ERROR", "MISSING":
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if found := findSyntaxProblemNode(n.Child(i)); found != nil {
			return found
		}
	}
	return nil
}

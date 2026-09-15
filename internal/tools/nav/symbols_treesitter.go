//go:build cgo

// tree-sitter Go symbols (tier 2). Built only when CGO is enabled; otherwise
// symbols_notreesitter.go provides a no-op stub and regex fallback runs.

package nav

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

func goSymbolsViaTreeSitter(ctx context.Context, src []byte) ([]Symbol, bool) {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, false
	}

	var out []Symbol

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type() {
		case "function_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = firstNamedOfType(n, "identifier")
			}
			if nameNode != nil {
				name := strings.TrimSpace(nameNode.Content(src))
				if name != "" {
					out = append(out, symbolFromNode(name, "function", n))
				}
			}

		case "method_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = firstNamedOfType(n, "field_identifier")
			}
			if nameNode != nil {
				name := strings.TrimSpace(nameNode.Content(src))
				if name != "" {
					out = append(out, symbolFromNode(name, "method", n))
				}
			}

		case "type_spec":
			nameNode := n.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = firstNamedOfType(n, "type_identifier")
			}
			if nameNode != nil {
				name := strings.TrimSpace(nameNode.Content(src))
				if name != "" {
					out = append(out, symbolFromNode(name, "type", n))
				}
			}
		}

		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}

	walk(root)
	return out, true
}

// symbolFromNode converts tree-sitter's 0-based points to the 1-based
// positions Symbol carries.
func symbolFromNode(name, kind string, n *sitter.Node) Symbol {
	sp := n.StartPoint()
	ep := n.EndPoint()
	return Symbol{
		Name: name, Kind: kind,
		StartLine: int(sp.Row) + 1, StartCol: int(sp.Column) + 1,
		EndLine: int(ep.Row) + 1, EndCol: int(ep.Column) + 1,
	}
}

func firstNamedOfType(n *sitter.Node, typ string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c != nil && c.Type() == typ {
			return c
		}
	}
	return nil
}

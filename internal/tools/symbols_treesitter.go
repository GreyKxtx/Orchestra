//go:build cgo

package tools

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"

	"github.com/orchestra/orchestra/internal/ops"
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
					out = append(out, Symbol{Name: name, Kind: "function", Range: rangeFromNode(n)})
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
					out = append(out, Symbol{Name: name, Kind: "method", Range: rangeFromNode(n)})
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
					out = append(out, Symbol{Name: name, Kind: "type", Range: rangeFromNode(n)})
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

func rangeFromNode(n *sitter.Node) *ops.Range {
	if n == nil {
		return nil
	}
	sp := n.StartPoint()
	ep := n.EndPoint()
	return &ops.Range{
		Start: ops.Position{Line: int(sp.Row), Col: int(sp.Column)},
		End:   ops.Position{Line: int(ep.Row), Col: int(ep.Column)},
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


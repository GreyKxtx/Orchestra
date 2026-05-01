package ckg

import (
	"context"
	"os"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// Language maps extensions to their name for the DB
func LanguageFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts":
		return "typescript"
	case ".js":
		return "javascript"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp":
		return "cpp"
	case ".rs":
		return "rust"
	default:
		return "unknown"
	}
}

// ParseFile parses a source file using Tree-sitter and extracts structural nodes
// and their line coordinate boundaries for the Code Knowledge Graph.
func ParseFile(ctx context.Context, filePath string) ([]Node, []Edge, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	ext := filepath.Ext(filePath)
	lang := getSitterLanguage(ext)
	if lang == nil {
		return nil, nil, nil // Unsupported language natively by parser
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil, err
	}
	defer tree.Close()

	root := tree.RootNode()

	var nodes []Node
	var edges []Edge

	// Execute language-specific queries to extract nodes
	queries := getLanguageQueries(ext)
	for _, qStr := range queries {
		q, err := sitter.NewQuery([]byte(qStr), lang)
		if err != nil {
			continue // Skip invalid queries
		}

		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			var name string
			var defNode *sitter.Node

			for _, c := range m.Captures {
				captureName := q.CaptureNameForId(c.Index)
				if captureName == "name" {
					name = c.Node.Content(src)
				} else if captureName == "def" {
					defNode = c.Node
				}
			}

			if name != "" && defNode != nil {
				nodeType := inferNodeType(defNode)
				
				if defNode.Type() == "method_declaration" && ext == ".go" {
					recvNode := defNode.ChildByFieldName("receiver")
					if recvNode != nil {
						recvType := extractGoReceiverType(recvNode, src)
						if recvType != "" {
							name = recvType + "." + name
						}
					}
				}

				nodes = append(nodes, Node{
					Name:      name,
					Type:      nodeType,
					LineStart: int(defNode.StartPoint().Row) + 1,
					LineEnd:   int(defNode.EndPoint().Row) + 1,
				})
			}
		}
	}

	// 2. Extract edges (method calls)
	callQueries := getCallQueries(ext)
	for _, qStr := range callQueries {
		q, err := sitter.NewQuery([]byte(qStr), lang)
		if err != nil {
			continue
		}

		qc := sitter.NewQueryCursor()
		qc.Exec(q, root)

		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			var calledName string
			var callNode *sitter.Node

			for _, c := range m.Captures {
				captureName := q.CaptureNameForId(c.Index)
				if captureName == "called_name" {
					calledName = c.Node.Content(src)
				} else if captureName == "call" {
					callNode = c.Node
				}
			}

			if calledName != "" && callNode != nil {
				// We don't know exactly who is calling (the source_id) until we link
				// it to the parent Node. For now, we store edges temporarily by finding
				// which node's line boundaries contain this call.
				callLine := int(callNode.StartPoint().Row) + 1
				
				// Find parent node
				var sourceName string
				for _, n := range nodes {
					if callLine >= n.LineStart && callLine <= n.LineEnd {
						sourceName = n.Name
						break
					}
				}

				if sourceName != "" {
					// We use a temporary representation. In the Store we will map Names to IDs.
					// Since our SaveFileNodes assigns IDs and we might reference nodes outside this file,
					// we actually need the Store to link edges by Name, or we store string names.
					// Let's modify Edge struct logic or store string names for now.
					edges = append(edges, Edge{
						SourceName: sourceName,
						TargetName: calledName,
						Relation:   "calls",
					})
				}
			}
		}
	}

	return nodes, edges, nil
}

func getSitterLanguage(ext string) *sitter.Language {
	switch ext {
	case ".go":
		return golang.GetLanguage()
	default:
		return nil
	}
}

func getLanguageQueries(ext string) []string {
	switch ext {
	case ".go":
		return []string{
			`(function_declaration name: (identifier) @name) @def`,
			`(method_declaration name: (field_identifier) @name) @def`,
			`(type_spec name: (type_identifier) @name) @def`,
		}
	default:
		return nil
	}
}

func getCallQueries(ext string) []string {
	switch ext {
	case ".go":
		return []string{
			// A simple call to a function
			`(call_expression function: (identifier) @called_name) @call`,
			// A call to a method (e.g. obj.Method())
			`(call_expression function: (selector_expression field: (field_identifier) @called_name)) @call`,
		}
	default:
		return nil
	}
}

func inferNodeType(defNode *sitter.Node) string {
	switch defNode.Type() {
	case "function_declaration":
		return "func"
	case "method_declaration":
		return "method"
	case "type_spec":
		typNode := defNode.ChildByFieldName("type")
		if typNode != nil {
			if typNode.Type() == "struct_type" {
				return "struct"
			}
			if typNode.Type() == "interface_type" {
				return "interface"
			}
		}
		return "type"
	default:
		return "symbol"
	}
}

func extractGoReceiverType(recvNode *sitter.Node, src []byte) string {
	// Drill down to find the type_identifier
	// It's under parameter_list -> parameter_declaration -> type -> (pointer_type ->) type_identifier
	for i := 0; i < int(recvNode.NamedChildCount()); i++ {
		paramDecl := recvNode.NamedChild(i)
		if paramDecl.Type() == "parameter_declaration" {
			typNode := paramDecl.ChildByFieldName("type")
			if typNode != nil {
				if typNode.Type() == "pointer_type" {
					typNode = typNode.NamedChild(0)
				}
				if typNode != nil && typNode.Type() == "type_identifier" {
					return typNode.Content(src)
				}
			}
		}
	}
	return ""
}

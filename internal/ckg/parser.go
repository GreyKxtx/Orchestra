package ckg

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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

// ParseFile parses a Go source file and returns:
//   - nodes (with FQN populated)
//   - edges (FQN→FQN; receiver-FQN to short calledName for calls;
//     packageFQN to imported-importpath for imports)
//   - pkgName (the `package foo` directive — for files.package column)
//   - error
//
// modulePath: from go.mod (may be empty for non-module workspaces)
// rootDir:    workspace root (absolute)
// filePath:   absolute path of the source file
func ParseFile(ctx context.Context, modulePath, rootDir, filePath string) ([]Node, []Edge, string, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, "", err
	}

	ext := filepath.Ext(filePath)
	lang := getSitterLanguage(ext)
	if lang == nil {
		return nil, nil, "", nil // Unsupported language natively by parser
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil, "", err
	}
	defer tree.Close()

	root := tree.RootNode()

	var nodes []Node
	var edges []Edge

	// Step 2: Find package name via query.
	var pkgName string
	if ext == ".go" {
		pkgQ, qErr := sitter.NewQuery([]byte(`(package_clause (package_identifier) @name)`), lang)
		if qErr == nil {
			pkgQC := sitter.NewQueryCursor()
			pkgQC.Exec(pkgQ, root)
			if m, ok := pkgQC.NextMatch(); ok {
				for _, c := range m.Captures {
					if pkgQ.CaptureNameForId(c.Index) == "name" {
						pkgName = c.Node.Content(src)
						break
					}
				}
			}
		}
	}

	// Step 3: Compute package FQN.
	pkgFQN := GoPackageFQN(modulePath, rootDir, filePath)

	// Step 4: Synthetic package-level node.
	nodes = append(nodes, Node{
		FQN:       pkgFQN,
		ShortName: pkgName,
		Kind:      "package",
		LineStart: 1,
		LineEnd:   1,
	})

	// Step 5: Run definition queries (function, method, type_spec).
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
				kind := inferNodeType(defNode)

				var recvType string
				if defNode.Type() == "method_declaration" && ext == ".go" {
					recvNode := defNode.ChildByFieldName("receiver")
					if recvNode != nil {
						recvType = extractGoReceiverType(recvNode, src)
					}
				}

				fqn := GoFQN(modulePath, rootDir, filePath, recvType, name)

				shortName := name
				if recvType != "" {
					shortName = recvType + "." + name
				}

				nodes = append(nodes, Node{
					FQN:       fqn,
					ShortName: shortName,
					Kind:      kind,
					LineStart: int(defNode.StartPoint().Row) + 1,
					LineEnd:   int(defNode.EndPoint().Row) + 1,
				})
			}
		}
	}

	// Step 6: Extract import edges.
	if ext == ".go" {
		impQ, qErr := sitter.NewQuery([]byte(`(import_spec path: (interpreted_string_literal) @path)`), lang)
		if qErr == nil {
			impQC := sitter.NewQueryCursor()
			impQC.Exec(impQ, root)
			for {
				m, ok := impQC.NextMatch()
				if !ok {
					break
				}
				for _, c := range m.Captures {
					if impQ.CaptureNameForId(c.Index) == "path" {
						rawPath := c.Node.Content(src)
						// Strip surrounding quotes from the interpreted string literal.
						importPath := strings.Trim(rawPath, `"`)
						if importPath != "" {
							edges = append(edges, Edge{
								SourceFQN: pkgFQN,
								TargetFQN: importPath,
								Relation:  "imports",
							})
						}
					}
				}
			}
		}
	}

	// Step 7: Run call queries and attach to parent node by line containment.
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
				callLine := int(callNode.StartPoint().Row) + 1

				// Find parent node by line containment; exclude "package" kind nodes.
				var sourceFQN string
				for _, n := range nodes {
					if n.Kind == "package" {
						continue
					}
					if callLine >= n.LineStart && callLine <= n.LineEnd {
						sourceFQN = n.FQN
						break
					}
				}

				if sourceFQN != "" {
					// NOTE: TargetFQN here is the short calledName identifier (best-effort).
					// Full FQN resolution of cross-package calls is a known limitation;
					// it will be resolved in a later sub-project when the store has full
					// FQN coverage and a resolver pass can link by short name.
					edges = append(edges, Edge{
						SourceFQN: sourceFQN,
						TargetFQN: calledName,
						Relation:  "calls",
					})
				}
			}
		}
	}

	return nodes, edges, pkgName, nil
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

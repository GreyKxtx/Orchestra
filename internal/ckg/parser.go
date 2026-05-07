package ckg

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/elixir"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// LanguageFromExt maps file extension to the language name stored in the DB.
func LanguageFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".kt", ".kts":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".ex", ".exs":
		return "elixir"
	default:
		return "unknown"
	}
}

// fileCtx holds parsing context for a single source file.
type fileCtx struct {
	ext        string
	modulePath string // Go: module path; Rust: crate name; others: unused
	rootDir    string
	filePath   string
	src        []byte
	lang       *sitter.Language
}

// containerDef represents a named block (class/struct/impl/interface/enum/trait)
// that can contain method definitions.
type containerDef struct {
	name      string
	kind      string
	lineStart int
	lineEnd   int
	emitNode  bool // if false, only used for method-to-container association
}

// containerQuerySpec pairs a tree-sitter query with the node kind it produces.
type containerQuerySpec struct {
	q        string
	kind     string
	emitNode bool
}

// defQuerySpec pairs a tree-sitter query with the default node kind it produces.
type defQuerySpec struct {
	q    string
	kind string // "func" or "method"; overridden to "method" when inside a container
}

// sitterLanguageFor returns the tree-sitter Language for the file extension.
func sitterLanguageFor(ext string) *sitter.Language {
	switch ext {
	case ".go":
		return golang.GetLanguage()
	case ".ts":
		return typescript.GetLanguage()
	case ".tsx":
		return tsx.GetLanguage()
	case ".js", ".jsx":
		return javascript.GetLanguage()
	case ".py":
		return python.GetLanguage()
	case ".rs":
		return rust.GetLanguage()
	case ".java":
		return java.GetLanguage()
	case ".c", ".h":
		return c.GetLanguage()
	case ".cpp", ".cc", ".cxx", ".hpp":
		return cpp.GetLanguage()
	case ".cs":
		return csharp.GetLanguage()
	case ".kt", ".kts":
		return kotlin.GetLanguage()
	case ".scala":
		return scala.GetLanguage()
	case ".rb":
		return ruby.GetLanguage()
	case ".php":
		return php.GetLanguage()
	case ".ex", ".exs":
		return elixir.GetLanguage()
	default:
		return nil
	}
}

// ParseFile parses a source file and returns nodes, edges, pkgName, and error.
//   - modulePath: Go module path or Rust crate name; empty for other languages.
//   - rootDir: absolute workspace root.
//   - filePath: absolute path to the source file.
func ParseFile(ctx context.Context, modulePath, rootDir, filePath string) ([]Node, []Edge, string, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, "", err
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	lang := sitterLanguageFor(ext)
	if lang == nil {
		return nil, nil, "", nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil, "", err
	}
	defer tree.Close()

	fc := &fileCtx{
		ext:        ext,
		modulePath: modulePath,
		rootDir:    rootDir,
		filePath:   filePath,
		src:        src,
		lang:       lang,
	}
	root := tree.RootNode()

	if ext == ".go" {
		return parseGoFile(ctx, fc, root)
	}
	return parseGenericFile(ctx, fc, root)
}

// ---- Go-specific parsing ----

func parseGoFile(_ context.Context, fc *fileCtx, root *sitter.Node) ([]Node, []Edge, string, error) {
	var nodes []Node
	var edges []Edge

	pkgName := singleCapture(fc, root, `(package_clause (package_identifier) @name)`, "name")
	pkgFQN := GoPackageFQN(fc.modulePath, fc.rootDir, fc.filePath)

	nodes = append(nodes, Node{
		FQN:       pkgFQN,
		ShortName: pkgName,
		Kind:      "package",
		LineStart: 1,
		LineEnd:   1,
	})

	goDefQueries := []string{
		`(function_declaration name: (identifier) @name) @def`,
		`(method_declaration name: (field_identifier) @name) @def`,
		`(type_spec name: (type_identifier) @name) @def`,
	}
	ctypes := complexityTypesFor(".go")

	for _, qStr := range goDefQueries {
		q, err := sitter.NewQuery([]byte(qStr), fc.lang)
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
			var name string
			var defNode *sitter.Node
			for _, c := range m.Captures {
				switch q.CaptureNameForId(c.Index) {
				case "name":
					name = c.Node.Content(fc.src)
				case "def":
					defNode = c.Node
				}
			}
			if name == "" || defNode == nil {
				continue
			}
			kind := inferGoKind(defNode)
			var recvType string
			if defNode.Type() == "method_declaration" {
				if recv := defNode.ChildByFieldName("receiver"); recv != nil {
					recvType = extractGoReceiverType(recv, fc.src)
				}
			}
			fqn := GoFQN(fc.modulePath, fc.rootDir, fc.filePath, recvType, name)
			shortName := name
			if recvType != "" {
				shortName = recvType + "." + name
			}
			lineStart := int(defNode.StartPoint().Row) + 1
			lineEnd := int(defNode.EndPoint().Row) + 1
			nodes = append(nodes, Node{
				FQN:        fqn,
				ShortName:  shortName,
				Kind:       kind,
				LineStart:  lineStart,
				LineEnd:    lineEnd,
				Complexity: countComplexity(root, lineStart, lineEnd, ctypes),
			})
		}
	}

	// Import edges
	impQ, err := sitter.NewQuery([]byte(`(import_spec path: (interpreted_string_literal) @path)`), fc.lang)
	if err == nil {
		impQC := sitter.NewQueryCursor()
		impQC.Exec(impQ, root)
		for {
			m, ok := impQC.NextMatch()
			if !ok {
				break
			}
			for _, c := range m.Captures {
				if impQ.CaptureNameForId(c.Index) == "path" {
					importPath := strings.Trim(c.Node.Content(fc.src), `"`)
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

	// Call edges
	goCallQueries := []string{
		`(call_expression function: (identifier) @called_name) @call`,
		`(call_expression function: (selector_expression field: (field_identifier) @called_name)) @call`,
	}
	for _, qStr := range goCallQueries {
		q, err := sitter.NewQuery([]byte(qStr), fc.lang)
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
				switch q.CaptureNameForId(c.Index) {
				case "called_name":
					calledName = c.Node.Content(fc.src)
				case "call":
					callNode = c.Node
				}
			}
			if calledName == "" || callNode == nil {
				continue
			}
			callLine := int(callNode.StartPoint().Row) + 1
			if sourceFQN := findEnclosingFQN(nodes, callLine); sourceFQN != "" {
				edges = append(edges, Edge{
					SourceFQN: sourceFQN,
					TargetFQN: calledName,
					Relation:  "calls",
				})
			}
		}
	}

	return nodes, edges, pkgName, nil
}

// ---- Generic polyglot parsing ----

func parseGenericFile(_ context.Context, fc *fileCtx, root *sitter.Node) ([]Node, []Edge, string, error) {
	var nodes []Node
	var edges []Edge

	// Extract language-specific package/module declaration.
	pkgDecl := scanPkgDecl(fc, root)

	pkgFQN := filePkgFQN(fc, pkgDecl)
	pkgShortName := pkgDecl
	if pkgShortName == "" {
		pkgShortName = strings.TrimSuffix(filepath.Base(fc.filePath), filepath.Ext(fc.filePath))
	}
	nodes = append(nodes, Node{
		FQN:       pkgFQN,
		ShortName: pkgShortName,
		Kind:      "package",
		LineStart: 1,
		LineEnd:   1,
	})

	// Scan class/struct/impl/interface/enum/trait containers.
	containers := scanContainersFor(fc, root)

	// Emit container nodes (struct, interface, type).
	for _, c := range containers {
		if !c.emitNode {
			continue
		}
		fqn := fileSymFQN(fc, pkgDecl, "", c.name)
		nodes = append(nodes, Node{
			FQN:       fqn,
			ShortName: c.name,
			Kind:      c.kind,
			LineStart: c.lineStart,
			LineEnd:   c.lineEnd,
		})
	}

	// Scan function/method definitions.
	ctypes := complexityTypesFor(fc.ext)
	for _, dq := range defQueriesFor(fc.ext) {
		q, err := sitter.NewQuery([]byte(dq.q), fc.lang)
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
			var name string
			var defNode *sitter.Node
			for _, c := range m.Captures {
				switch q.CaptureNameForId(c.Index) {
				case "name":
					name = c.Node.Content(fc.src)
				case "def":
					defNode = c.Node
				}
			}
			if name == "" || defNode == nil {
				continue
			}
			lineStart := int(defNode.StartPoint().Row) + 1
			lineEnd := int(defNode.EndPoint().Row) + 1
			container := findContainerAt(containers, lineStart)
			fqn := fileSymFQN(fc, pkgDecl, container, name)
			shortName := name
			if container != "" {
				shortName = container + "." + name
			}
			kind := dq.kind
			if container != "" && kind == "func" {
				kind = "method"
			}
			nodes = append(nodes, Node{
				FQN:        fqn,
				ShortName:  shortName,
				Kind:       kind,
				LineStart:  lineStart,
				LineEnd:    lineEnd,
				Complexity: countComplexity(root, lineStart, lineEnd, ctypes),
			})
		}
	}

	// Import edges.
	for _, qStr := range importQueriesFor(fc.ext) {
		q, err := sitter.NewQuery([]byte(qStr), fc.lang)
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
			for _, c := range m.Captures {
				if q.CaptureNameForId(c.Index) == "path" {
					importPath := cleanImportPath(c.Node.Content(fc.src), fc.ext)
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

	// Call edges.
	for _, qStr := range callQueriesFor(fc.ext) {
		q, err := sitter.NewQuery([]byte(qStr), fc.lang)
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
				switch q.CaptureNameForId(c.Index) {
				case "called_name":
					calledName = c.Node.Content(fc.src)
				case "call":
					callNode = c.Node
				}
			}
			if calledName == "" || callNode == nil {
				continue
			}
			callLine := int(callNode.StartPoint().Row) + 1
			if sourceFQN := findEnclosingFQN(nodes, callLine); sourceFQN != "" {
				edges = append(edges, Edge{
					SourceFQN: sourceFQN,
					TargetFQN: calledName,
					Relation:  "calls",
				})
			}
		}
	}

	return nodes, edges, pkgDecl, nil
}

// ---- Helpers ----

// scanPkgDecl extracts the package/module declaration string for languages that have one.
// Returns "" for languages that derive identity from file path (TS, JS, Python, Rust, Ruby, Elixir).
func scanPkgDecl(fc *fileCtx, root *sitter.Node) string {
	switch fc.ext {
	case ".java":
		for _, qStr := range []string{
			`(package_declaration (scoped_identifier) @path)`,
			`(package_declaration (identifier) @path)`,
		} {
			if v := singleCapture(fc, root, qStr, "path"); v != "" {
				return v
			}
		}
	case ".cs":
		for _, qStr := range []string{
			`(namespace_declaration name: (qualified_name) @path)`,
			`(namespace_declaration name: (identifier) @path)`,
		} {
			if v := singleCapture(fc, root, qStr, "path"); v != "" {
				return v
			}
		}
	case ".kt", ".kts":
		for _, qStr := range []string{
			`(package_header (dot_qualified_expression) @path)`,
			`(package_header (simple_identifier) @path)`,
		} {
			if v := singleCapture(fc, root, qStr, "path"); v != "" {
				return v
			}
		}
	case ".scala":
		for _, qStr := range []string{
			`(package_clause (package_identifier) @path)`,
		} {
			if v := singleCapture(fc, root, qStr, "path"); v != "" {
				return v
			}
		}
	case ".php":
		for _, qStr := range []string{
			`(namespace_definition (namespace_name) @path)`,
		} {
			if v := singleCapture(fc, root, qStr, "path"); v != "" {
				return v
			}
		}
	}
	return ""
}

// filePkgFQN returns the file/module-level FQN for the given language.
func filePkgFQN(fc *fileCtx, pkgDecl string) string {
	switch fc.ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return TsPackageFQN(fc.rootDir, fc.filePath)
	case ".py":
		return PyPackageFQN(fc.rootDir, fc.filePath)
	case ".rs":
		return RustPackageFQN(fc.modulePath, fc.rootDir, fc.filePath)
	case ".java":
		return JavaPackageFQN(pkgDecl)
	case ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return CFileFQN(fc.rootDir, fc.filePath)
	case ".cs":
		return CSharpPackageFQN(pkgDecl)
	case ".kt", ".kts":
		return KotlinPackageFQN(pkgDecl)
	case ".scala":
		return ScalaPackageFQN(pkgDecl)
	case ".rb":
		return RubyPackageFQN(fc.rootDir, fc.filePath)
	case ".php":
		return PhpPackageFQN(pkgDecl)
	case ".ex", ".exs":
		return ElixirPackageFQN(fc.rootDir, fc.filePath)
	default:
		return ""
	}
}

// fileSymFQN returns the FQN for a named symbol in the given language.
func fileSymFQN(fc *fileCtx, pkgDecl, container, symbol string) string {
	switch fc.ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return TsFQN(fc.rootDir, fc.filePath, container, symbol)
	case ".py":
		return PyFQN(fc.rootDir, fc.filePath, container, symbol)
	case ".rs":
		return RustFQN(fc.modulePath, fc.rootDir, fc.filePath, container, symbol)
	case ".java":
		return JavaFQN(pkgDecl, container, symbol)
	case ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return CFQN(fc.rootDir, fc.filePath, container, symbol)
	case ".cs":
		return CSharpFQN(pkgDecl, container, symbol)
	case ".kt", ".kts":
		return KotlinFQN(pkgDecl, container, symbol)
	case ".scala":
		return ScalaFQN(pkgDecl, container, symbol)
	case ".rb":
		return RubyFQN(fc.rootDir, fc.filePath, container, symbol)
	case ".php":
		return PhpFQN(pkgDecl, container, symbol)
	case ".ex", ".exs":
		return ElixirFQN(fc.rootDir, fc.filePath, container, symbol)
	default:
		return symbol
	}
}

// scanContainersFor runs container queries and returns all class/struct/impl/etc. blocks.
func scanContainersFor(fc *fileCtx, root *sitter.Node) []containerDef {
	specs := containerQueriesFor(fc.ext)
	if len(specs) == 0 {
		return nil
	}
	var result []containerDef
	for _, spec := range specs {
		q, err := sitter.NewQuery([]byte(spec.q), fc.lang)
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
			var name string
			var defNode *sitter.Node
			for _, c := range m.Captures {
				switch q.CaptureNameForId(c.Index) {
				case "name":
					name = c.Node.Content(fc.src)
				case "def":
					defNode = c.Node
				}
			}
			if name == "" || defNode == nil {
				continue
			}
			result = append(result, containerDef{
				name:      name,
				kind:      spec.kind,
				lineStart: int(defNode.StartPoint().Row) + 1,
				lineEnd:   int(defNode.EndPoint().Row) + 1,
				emitNode:  spec.emitNode,
			})
		}
	}
	return result
}

// findContainerAt returns the innermost container name spanning line, or "".
func findContainerAt(containers []containerDef, line int) string {
	best, bestStart := "", 0
	for _, c := range containers {
		if line >= c.lineStart && line <= c.lineEnd && c.lineStart > bestStart {
			bestStart = c.lineStart
			best = c.name
		}
	}
	return best
}

// findEnclosingFQN returns the FQN of the non-package node that spans line, or "".
func findEnclosingFQN(nodes []Node, line int) string {
	for _, n := range nodes {
		if n.Kind == "package" {
			continue
		}
		if line >= n.LineStart && line <= n.LineEnd {
			return n.FQN
		}
	}
	return ""
}

// singleCapture runs qStr against root and returns the first match for the named capture.
func singleCapture(fc *fileCtx, root *sitter.Node, qStr, capture string) string {
	q, err := sitter.NewQuery([]byte(qStr), fc.lang)
	if err != nil {
		return ""
	}
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)
	if m, ok := qc.NextMatch(); ok {
		for _, c := range m.Captures {
			if q.CaptureNameForId(c.Index) == capture {
				return c.Node.Content(fc.src)
			}
		}
	}
	return ""
}

// cleanImportPath strips quotes from TS/JS string literals; other languages need no cleaning.
func cleanImportPath(raw, ext string) string {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.Trim(raw, `"'`+"`")
	default:
		return raw
	}
}

// ---- Query dispatch ----

func containerQueriesFor(ext string) []containerQuerySpec {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return []containerQuerySpec{
			{q: `(class_declaration name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(abstract_class_declaration name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(interface_declaration name: (type_identifier) @name) @def`, kind: "interface", emitNode: true},
			{q: `(enum_declaration name: (identifier) @name) @def`, kind: "type", emitNode: true},
		}
	case ".py":
		return []containerQuerySpec{
			{q: `(class_definition name: (identifier) @name) @def`, kind: "struct", emitNode: true},
		}
	case ".rs":
		return []containerQuerySpec{
			{q: `(struct_item name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_item name: (type_identifier) @name) @def`, kind: "type", emitNode: true},
			{q: `(trait_item name: (type_identifier) @name) @def`, kind: "interface", emitNode: true},
			// impl blocks: used only for method-to-container association, not emitted as nodes
			{q: `(impl_item type: (type_identifier) @name) @def`, kind: "struct", emitNode: false},
		}
	case ".java":
		return []containerQuerySpec{
			{q: `(class_declaration name: (identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(interface_declaration name: (identifier) @name) @def`, kind: "interface", emitNode: true},
			{q: `(enum_declaration name: (identifier) @name) @def`, kind: "type", emitNode: true},
			{q: `(annotation_type_declaration name: (identifier) @name) @def`, kind: "type", emitNode: true},
			{q: `(record_declaration name: (identifier) @name) @def`, kind: "struct", emitNode: true},
		}
	case ".c", ".h":
		return []containerQuerySpec{
			{q: `(struct_specifier name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(union_specifier name: (type_identifier) @name) @def`, kind: "type", emitNode: true},
			{q: `(enum_specifier name: (type_identifier) @name) @def`, kind: "type", emitNode: true},
		}
	case ".cpp", ".cc", ".cxx", ".hpp":
		return []containerQuerySpec{
			{q: `(class_specifier name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(struct_specifier name: (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_specifier name: (type_identifier) @name) @def`, kind: "type", emitNode: true},
		}
	case ".cs":
		return []containerQuerySpec{
			{q: `(class_declaration name: (identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(interface_declaration name: (identifier) @name) @def`, kind: "interface", emitNode: true},
			{q: `(struct_declaration name: (identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_declaration name: (identifier) @name) @def`, kind: "type", emitNode: true},
			{q: `(record_declaration name: (identifier) @name) @def`, kind: "struct", emitNode: true},
		}
	case ".kt", ".kts":
		return []containerQuerySpec{
			{q: `(class_declaration (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(interface_declaration (type_identifier) @name) @def`, kind: "interface", emitNode: true},
			{q: `(object_declaration (type_identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_class (type_identifier) @name) @def`, kind: "type", emitNode: true},
		}
	case ".scala":
		return []containerQuerySpec{
			{q: `(class_definition name: (identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(trait_definition name: (identifier) @name) @def`, kind: "interface", emitNode: true},
			{q: `(object_definition name: (identifier) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_definition name: (identifier) @name) @def`, kind: "type", emitNode: true},
		}
	case ".rb":
		return []containerQuerySpec{
			{q: `(class name: (constant) @name) @def`, kind: "struct", emitNode: true},
			{q: `(module name: (constant) @name) @def`, kind: "struct", emitNode: true},
			{q: `(singleton_class) @def`, kind: "struct", emitNode: false},
		}
	case ".php":
		return []containerQuerySpec{
			{q: `(class_declaration name: (name) @name) @def`, kind: "struct", emitNode: true},
			{q: `(interface_declaration name: (name) @name) @def`, kind: "interface", emitNode: true},
			{q: `(trait_declaration name: (name) @name) @def`, kind: "struct", emitNode: true},
			{q: `(enum_declaration name: (name) @name) @def`, kind: "type", emitNode: true},
		}
	case ".ex", ".exs":
		return []containerQuerySpec{
			// defmodule MyApp.Module do ... end
			{q: `(call target: (identifier) arguments: (arguments (alias) @name)) @def`, kind: "struct", emitNode: true},
		}
	}
	return nil
}

func defQueriesFor(ext string) []defQuerySpec {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return []defQuerySpec{
			{q: `(function_declaration name: (identifier) @name) @def`, kind: "func"},
			{q: `(method_definition name: (property_identifier) @name) @def`, kind: "method"},
			{q: `(method_definition name: (private_property_identifier) @name) @def`, kind: "method"},
			{q: `(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @def`, kind: "func"},
			{q: `(lexical_declaration (variable_declarator name: (identifier) @name value: (function_expression))) @def`, kind: "func"},
			{q: `(variable_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @def`, kind: "func"},
		}
	case ".py":
		return []defQuerySpec{
			{q: `(function_definition name: (identifier) @name) @def`, kind: "func"},
		}
	case ".rs":
		return []defQuerySpec{
			{q: `(function_item name: (identifier) @name) @def`, kind: "func"},
		}
	case ".java":
		return []defQuerySpec{
			{q: `(method_declaration name: (identifier) @name) @def`, kind: "method"},
			{q: `(constructor_declaration name: (identifier) @name) @def`, kind: "func"},
		}
	case ".c", ".h":
		return []defQuerySpec{
			{q: `(function_definition declarator: (function_declarator declarator: (identifier) @name)) @def`, kind: "func"},
		}
	case ".cpp", ".cc", ".cxx", ".hpp":
		return []defQuerySpec{
			{q: `(function_definition declarator: (function_declarator declarator: (identifier) @name)) @def`, kind: "func"},
			{q: `(function_definition declarator: (function_declarator declarator: (qualified_identifier name: (identifier) @name))) @def`, kind: "method"},
		}
	case ".cs":
		return []defQuerySpec{
			{q: `(method_declaration name: (identifier) @name) @def`, kind: "method"},
			{q: `(constructor_declaration name: (identifier) @name) @def`, kind: "func"},
			{q: `(local_function_statement name: (identifier) @name) @def`, kind: "func"},
		}
	case ".kt", ".kts":
		return []defQuerySpec{
			{q: `(function_declaration (simple_identifier) @name) @def`, kind: "func"},
		}
	case ".scala":
		return []defQuerySpec{
			{q: `(function_definition name: (identifier) @name) @def`, kind: "func"},
		}
	case ".rb":
		return []defQuerySpec{
			{q: `(method name: (identifier) @name) @def`, kind: "func"},
			{q: `(singleton_method name: (identifier) @name) @def`, kind: "func"},
		}
	case ".php":
		return []defQuerySpec{
			{q: `(function_definition name: (name) @name) @def`, kind: "func"},
			{q: `(method_declaration name: (name) @name) @def`, kind: "method"},
		}
	case ".ex", ".exs":
		return []defQuerySpec{
			// def/defp my_func(...) do...end — inner call target is the function name
			{q: `(call target: (identifier) arguments: (arguments (call target: (identifier) @name))) @def`, kind: "func"},
		}
	}
	return nil
}

func importQueriesFor(ext string) []string {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return []string{
			`(import_statement source: (string) @path)`,
		}
	case ".py":
		return []string{
			`(import_statement (dotted_name) @path)`,
			`(import_from_statement module_name: (dotted_name) @path)`,
		}
	case ".rs":
		return []string{
			`(use_declaration argument: (scoped_identifier) @path)`,
			`(use_declaration argument: (identifier) @path)`,
			`(use_declaration argument: (scoped_use_list path: (scoped_identifier) @path))`,
		}
	case ".java":
		return []string{
			`(import_declaration (scoped_identifier) @path)`,
			`(import_declaration (identifier) @path)`,
		}
	case ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return []string{
			`(preproc_include path: (string_literal) @path)`,
			`(preproc_include path: (system_lib_string) @path)`,
		}
	case ".cs":
		return []string{
			`(using_directive (qualified_name) @path)`,
			`(using_directive (identifier) @path)`,
		}
	case ".kt", ".kts":
		return []string{
			`(import_header (dot_qualified_expression) @path)`,
			`(import_header (simple_identifier) @path)`,
		}
	case ".scala":
		return []string{
			`(import_declaration (stable_identifier) @path)`,
			`(import_declaration (identifier) @path)`,
		}
	case ".php":
		return []string{
			`(namespace_use_declaration (namespace_use_clause (qualified_name) @path))`,
		}
	}
	return nil
}

func callQueriesFor(ext string) []string {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		return []string{
			`(call_expression function: (identifier) @called_name) @call`,
			`(call_expression function: (member_expression property: (property_identifier) @called_name)) @call`,
		}
	case ".py":
		return []string{
			`(call function: (identifier) @called_name) @call`,
			`(call function: (attribute attribute: (identifier) @called_name)) @call`,
		}
	case ".rs":
		return []string{
			`(call_expression function: (identifier) @called_name) @call`,
			`(call_expression function: (scoped_identifier name: (identifier) @called_name)) @call`,
			`(method_call_expression name: (field_identifier) @called_name) @call`,
		}
	case ".java":
		return []string{
			`(method_invocation name: (identifier) @called_name) @call`,
		}
	case ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return []string{
			`(call_expression function: (identifier) @called_name) @call`,
		}
	case ".cs":
		return []string{
			`(invocation_expression function: (identifier) @called_name) @call`,
			`(invocation_expression function: (member_access_expression name: (identifier) @called_name)) @call`,
		}
	case ".kt", ".kts":
		return []string{
			`(call_expression (simple_identifier) @called_name) @call`,
		}
	case ".scala":
		return []string{
			`(call_expression (identifier) @called_name) @call`,
		}
	case ".rb":
		return []string{
			`(call method: (identifier) @called_name) @call`,
		}
	case ".php":
		return []string{
			`(function_call_expression function: (name) @called_name) @call`,
			`(member_call_expression name: (name) @called_name) @call`,
		}
	case ".ex", ".exs":
		return []string{
			`(call target: (identifier) @called_name) @call`,
		}
	}
	return nil
}

// ---- Complexity ----

func complexityTypesFor(ext string) map[string]bool {
	switch ext {
	case ".go":
		return map[string]bool{
			"if_statement":          true,
			"for_statement":         true,
			"switch_statement":      true,
			"type_switch_statement": true,
			"select_statement":      true,
			"case_clause":           true,
		}
	case ".ts", ".tsx", ".js", ".jsx":
		return map[string]bool{
			"if_statement":       true,
			"for_statement":      true,
			"for_in_statement":   true,
			"for_of_statement":   true,
			"while_statement":    true,
			"do_statement":       true,
			"switch_case":        true,
			"catch_clause":       true,
			"ternary_expression": true,
		}
	case ".py":
		return map[string]bool{
			"if_statement":    true,
			"elif_clause":     true,
			"for_statement":   true,
			"while_statement": true,
			"except_clause":   true,
			"with_statement":  true,
		}
	case ".rs":
		return map[string]bool{
			"if_expression":        true,
			"if_let_expression":    true,
			"for_expression":       true,
			"while_expression":     true,
			"while_let_expression": true,
			"loop_expression":      true,
			"match_arm":            true,
		}
	case ".java":
		return map[string]bool{
			"if_statement":           true,
			"for_statement":          true,
			"enhanced_for_statement": true,
			"while_statement":        true,
			"do_statement":           true,
			"catch_clause":           true,
			"switch_label":           true,
			"conditional_expression": true,
		}
	case ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return map[string]bool{
			"if_statement":           true,
			"for_statement":          true,
			"while_statement":        true,
			"do_statement":           true,
			"case_statement":         true,
			"conditional_expression": true,
		}
	case ".cs":
		return map[string]bool{
			"if_statement":           true,
			"for_statement":          true,
			"foreach_statement":      true,
			"while_statement":        true,
			"do_statement":           true,
			"switch_section":         true,
			"catch_clause":           true,
			"conditional_expression": true,
		}
	case ".kt", ".kts":
		return map[string]bool{
			"if_expression":      true,
			"for_statement":      true,
			"while_statement":    true,
			"do_while_statement": true,
			"when_entry":         true,
			"catch_block":        true,
		}
	case ".scala":
		return map[string]bool{
			"if_expression":    true,
			"for_expression":   true,
			"while_expression": true,
			"case_clause":      true,
			"catch_clause":     true,
		}
	case ".rb":
		return map[string]bool{
			"if":     true,
			"elsif":  true,
			"unless": true,
			"while":  true,
			"until":  true,
			"for":    true,
			"when":   true,
			"rescue": true,
		}
	case ".php":
		return map[string]bool{
			"if_statement":           true,
			"for_statement":          true,
			"foreach_statement":      true,
			"while_statement":        true,
			"do_statement":           true,
			"switch_statement":       true,
			"catch_clause":           true,
			"conditional_expression": true,
		}
	case ".ex", ".exs":
		return map[string]bool{
			"if":     true,
			"unless": true,
			"cond":   true,
			"case":   true,
		}
	}
	return nil
}

// countComplexity counts AST decision nodes in [lineStart, lineEnd] whose type is in kinds.
func countComplexity(root *sitter.Node, lineStart, lineEnd int, kinds map[string]bool) int {
	if len(kinds) == 0 {
		return 0
	}
	count := 0
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		nStart := int(n.StartPoint().Row) + 1
		nEnd := int(n.EndPoint().Row) + 1
		if nEnd < lineStart || nStart > lineEnd {
			return
		}
		if kinds[n.Type()] && nStart >= lineStart && nStart <= lineEnd {
			count++
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return count
}

// ---- Go-specific helpers ----

func inferGoKind(defNode *sitter.Node) string {
	switch defNode.Type() {
	case "function_declaration":
		return "func"
	case "method_declaration":
		return "method"
	case "type_spec":
		typNode := defNode.ChildByFieldName("type")
		if typNode != nil {
			switch typNode.Type() {
			case "struct_type":
				return "struct"
			case "interface_type":
				return "interface"
			}
		}
		return "type"
	default:
		return "symbol"
	}
}

func extractGoReceiverType(recvNode *sitter.Node, src []byte) string {
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

package ckg

import (
	"context"
	"os"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// React components in the graph's web picture.
//
// A component styles its elements through className and the stylesheets it
// imports (import './App.css'), where a page has a class attribute and a
// <link>. The same relations follow:
//
//	file     -imports->   stylesheet   import './App.css' (a change to the
//	                                    sheet parses the component again)
//	element  -styled_by-> rule         "<rel>::.class", "<rel>::#id"
//
// Only the classes and ids a rule of an imported sheet styles become
// elements: a utility-class component (Tailwind) would otherwise add a symbol
// per utility, none of them styled by anything the graph can see.

// isJSXHost reports the files whose JSX may carry className.
func isJSXHost(ext string) bool {
	switch ext {
	case ".jsx", ".tsx", ".js", ".ts":
		return true
	}
	return false
}

var (
	cssClassName     = regexp.MustCompile(`^-?[_a-zA-Z][_a-zA-Z0-9-]*$`)
	templateSubstExp = regexp.MustCompile(`\$\{[^}]*\}`)
)

// jsxStyleLinks returns the elements and relations of a component's styling.
func jsxStyleLinks(ctx context.Context, fc *fileCtx, root *sitter.Node, fileFQN string) ([]Node, []Edge) {
	var sheets []string
	var rules []cssRule
	walkTree(root, func(n *sitter.Node) {
		if n.Type() != "import_statement" {
			return
		}
		src := n.ChildByFieldName("source")
		if src == nil {
			return
		}
		spec := unquoteJS(src.Content(fc.src))
		if !strings.HasSuffix(strings.ToLower(spec), ".css") || !strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/") {
			return // a package's stylesheet is not the workspace's
		}
		r, abs := localRef(fc.rootDir, fc.filePath, spec)
		if r == "" {
			return
		}
		sheets = append(sheets, r)
		real, _, err := fsutil.ResolveInWorkspace(fc.rootDir, abs)
		if err != nil {
			return
		}
		if data, err := os.ReadFile(real); err == nil {
			rules = append(rules, cssRules(ctx, r, data, 0)...)
		}
	})
	if len(sheets) == 0 {
		return nil, nil
	}
	var edges []Edge
	for _, s := range sheets {
		edges = append(edges, Edge{SourceFQN: fileFQN, TargetFQN: s, Relation: "imports"})
	}

	// What the rules style, by name.
	styledBy := map[string][]string{} // ".class" / "#id" → rule FQNs
	for _, r := range rules {
		for _, t := range r.targets {
			if t.id != "" {
				styledBy["#"+t.id] = append(styledBy["#"+t.id], r.fqn)
			}
			for _, c := range t.classes {
				styledBy["."+c] = append(styledBy["."+c], r.fqn)
			}
		}
	}

	var nodes []Node
	seen := map[string]bool{}
	walkTree(root, func(n *sitter.Node) {
		if n.Type() != "jsx_attribute" || n.NamedChildCount() < 2 {
			return
		}
		prefix := ""
		switch n.NamedChild(0).Content(fc.src) {
		case "className", "class":
			prefix = "."
		case "id":
			prefix = "#"
		default:
			return
		}
		line := int(n.StartPoint().Row) + 1
		for _, name := range jsxAttrStrings(n.NamedChild(1), fc.src) {
			if prefix == "#" && strings.ContainsAny(name, " \t") {
				continue
			}
			key := prefix + name
			targets, styled := styledBy[key]
			if !styled {
				continue
			}
			fqn := fileFQN + "::" + key
			if !seen[fqn] {
				seen[fqn] = true
				nodes = append(nodes, Node{FQN: fqn, ShortName: key, Kind: "element", LineStart: line, LineEnd: line})
				for _, rule := range targets {
					edges = append(edges, Edge{SourceFQN: fqn, TargetFQN: rule, Relation: "styled_by"})
				}
			}
		}
	})
	return nodes, dedupeEdges(edges)
}

// jsxAttrStrings returns the names an attribute value spells out: the words of
// its string literals, wherever they sit in it — "a b", {"a"},
// {clsx("a", on && "b")}, {`a ${x}`}.
func jsxAttrStrings(v *sitter.Node, src []byte) []string {
	var out []string
	walkTree(v, func(n *sitter.Node) {
		var text string
		switch n.Type() {
		case "string":
			text = unquoteJS(n.Content(src))
		case "template_string":
			text = templateSubstExp.ReplaceAllString(strings.Trim(n.Content(src), "`"), " ")
		default:
			return
		}
		for _, w := range strings.Fields(text) {
			if cssClassName.MatchString(w) {
				out = append(out, w)
			}
		}
	})
	return out
}

func unquoteJS(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		switch s[0] {
		case '"', '\'', '`':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

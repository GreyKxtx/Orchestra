package ckg

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/javascript"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// Web pages and stylesheets in the graph.
//
// A page's logic usually lives in its inline <script>, so a page is parsed for
// that script (as JavaScript, on the page's own lines) and for what a script
// or a stylesheet can name: elements with an id, the classes in use, and the
// tags some rule styles. Rules — from inline <style> and from .css files —
// are symbols of their own. The relations:
//
//	element  -styled_by-> rule      a rule whose selector ends on the element
//	function -uses->      element   getElementById / querySelector / …
//	page     -imports->   file      <script src>, <link rel=stylesheet>
//
// FQNs: "<rel>::#id", "<rel>::.class", "<rel>::<tag>" for elements,
// "<rel>::css <selector>" for rules ("~2", "~3" … for a selector repeated in
// one file), and the file's path for the file itself.

// isWebExt reports the web files the graph parses itself.
func isWebExt(ext string) bool {
	switch ext {
	case ".html", ".htm", ".css":
		return true
	}
	return false
}

// Indexable reports whether the graph parses files with this extension.
// SitterLanguageFor stays the languages with symbol queries — what the AST
// rename and the syntax check can drive.
func Indexable(ext string) bool {
	ext = strings.ToLower(ext)
	return sitterLanguageFor(ext) != nil || isWebExt(ext)
}

// webRelPath is a file's path as the graph names it.
func webRelPath(rootDir, filePath string) string {
	return tsModuleKey(rootDir, filePath)
}

// ---- stylesheets ----

// cssRule is one rule of a stylesheet: what it selects and where.
type cssRule struct {
	fqn       string
	selector  string
	lineStart int
	lineEnd   int
	// targets are the last compounds of the rule's selectors: what each one
	// actually styles.
	targets []cssCompound
}

// cssCompound is the last compound of a selector, reduced to what names an
// element: ".panel button:hover" styles <button>; "a.btn#go" an id and a class.
type cssCompound struct {
	tag     string
	id      string
	classes []string
}

// cssRules reads a stylesheet's rules. lineOffset moves them onto the lines
// of the file that holds the stylesheet (an inline <style>).
func cssRules(ctx context.Context, rel string, src []byte, lineOffset int) []cssRule {
	parser := sitter.NewParser()
	parser.SetLanguage(css.GetLanguage())
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil
	}
	defer tree.Close()
	var out []cssRule
	seen := map[string]int{}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "keyframes_statement":
			return // "from" / "50%" select no element
		case "rule_set":
			sel := n.NamedChild(0)
			if sel == nil || sel.Type() != "selectors" {
				return
			}
			text := strings.Join(strings.Fields(sel.Content(src)), " ")
			if text == "" {
				return
			}
			seen[text]++
			fqn := rel + "::css " + text
			if k := seen[text]; k > 1 {
				fqn += "~" + strconv.Itoa(k)
			}
			out = append(out, cssRule{
				fqn:       fqn,
				selector:  text,
				lineStart: int(n.StartPoint().Row) + 1 + lineOffset,
				lineEnd:   int(n.EndPoint().Row) + 1 + lineOffset,
				targets:   selectorTargets(text),
			})
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return out
}

var (
	cssCombinator = regexp.MustCompile(`\s*[>+~]\s*|\s+`)
	cssPseudo     = regexp.MustCompile(`::?[a-zA-Z-]+(\([^)]*\))?`)
	cssAttr       = regexp.MustCompile(`\[[^\]]*\]`)
	cssIDOrClass  = regexp.MustCompile(`([#.])([a-zA-Z_][\w-]*)`)
	cssTag        = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*`)
)

// selectorTargets returns the last compound of each selector in a list.
func selectorTargets(list string) []cssCompound {
	var out []cssCompound
	for _, sel := range strings.Split(list, ",") {
		parts := cssCombinator.Split(strings.TrimSpace(sel), -1)
		last := ""
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.TrimSpace(parts[i]) != "" {
				last = parts[i]
				break
			}
		}
		last = cssAttr.ReplaceAllString(cssPseudo.ReplaceAllString(last, ""), "")
		var c cssCompound
		c.tag = strings.ToLower(cssTag.FindString(last))
		for _, m := range cssIDOrClass.FindAllStringSubmatch(last, -1) {
			if m[1] == "#" {
				c.id = m[2]
			} else {
				c.classes = append(c.classes, m[2])
			}
		}
		if c.tag != "" || c.id != "" || len(c.classes) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// parseCSSSource parses a stylesheet: the file, and a symbol per rule.
func parseCSSSource(ctx context.Context, rootDir, filePath string, src []byte) ([]Node, []Edge, string, error) {
	rel := webRelPath(rootDir, filePath)
	nodes := []Node{{
		FQN:       rel,
		ShortName: strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		Kind:      "package",
		LineStart: 1,
		LineEnd:   1,
	}}
	for _, r := range cssRules(ctx, rel, src, 0) {
		nodes = append(nodes, Node{FQN: r.fqn, ShortName: r.selector, Kind: "style", LineStart: r.lineStart, LineEnd: r.lineEnd})
	}
	return nodes, nil, "", nil
}

// ---- pages ----

// htmlElement is an element of a page that a rule or a script can name.
type htmlElement struct {
	tag       string
	id        string
	classes   []string
	lineStart int
	lineEnd   int
}

// htmlPage is what a page holds, gathered in one walk.
type htmlPage struct {
	elements    []htmlElement
	scripts     []*sitter.Node // inline script bodies (raw_text)
	styles      []*sitter.Node // inline style bodies (raw_text)
	scriptSrcs  []string
	stylesheets []string
}

// attrs reads a start tag's attributes, names lower-cased.
func attrs(tag *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	for i := 0; i < int(tag.NamedChildCount()); i++ {
		a := tag.NamedChild(i)
		if a.Type() != "attribute" {
			continue
		}
		var name, value string
		for j := 0; j < int(a.NamedChildCount()); j++ {
			c := a.NamedChild(j)
			switch c.Type() {
			case "attribute_name":
				name = strings.ToLower(c.Content(src))
			case "attribute_value":
				value = c.Content(src)
			case "quoted_attribute_value":
				value = strings.Trim(c.Content(src), `"'`)
			}
		}
		if name != "" {
			out[name] = value
		}
	}
	return out
}

// isCodeScript reports a <script> type that holds JavaScript, not data or a
// template.
func isCodeScript(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "module", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript":
		return true
	}
	return false
}

func gatherPage(root *sitter.Node, src []byte) htmlPage {
	var p htmlPage
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "script_element", "style_element":
			var a map[string]string
			var body *sitter.Node
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "start_tag":
					a = attrs(c, src)
				case "raw_text":
					body = c
				}
			}
			if n.Type() == "style_element" {
				if body != nil {
					p.styles = append(p.styles, body)
				}
				return
			}
			if s := strings.TrimSpace(a["src"]); s != "" {
				p.scriptSrcs = append(p.scriptSrcs, s)
				return
			}
			if body != nil && isCodeScript(a["type"]) {
				p.scripts = append(p.scripts, body)
			}
			return
		case "start_tag", "self_closing_tag":
			a := attrs(n, src)
			tag := ""
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if c := n.NamedChild(i); c.Type() == "tag_name" {
					tag = strings.ToLower(c.Content(src))
				}
			}
			if tag == "link" && strings.Contains(" "+strings.ToLower(a["rel"])+" ", " stylesheet ") {
				if h := strings.TrimSpace(a["href"]); h != "" {
					p.stylesheets = append(p.stylesheets, h)
				}
			}
			el := n
			if n.Type() == "start_tag" && n.Parent() != nil {
				el = n.Parent()
			}
			p.elements = append(p.elements, htmlElement{
				tag:       tag,
				id:        strings.TrimSpace(a["id"]),
				classes:   strings.Fields(a["class"]),
				lineStart: int(el.StartPoint().Row) + 1,
				lineEnd:   int(el.EndPoint().Row) + 1,
			})
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return p
}

// localRef resolves a page's reference to a file of the workspace: the path
// the graph names it by, or "" for a URL, a data: link or a path that leaves
// the workspace.
func localRef(rootDir, pagePath, ref string) (rel, abs string) {
	ref = strings.TrimSpace(ref)
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") || strings.HasPrefix(ref, "data:") {
		return "", ""
	}
	if strings.HasPrefix(ref, "/") {
		abs = filepath.Join(rootDir, filepath.FromSlash(strings.TrimPrefix(ref, "/")))
	} else {
		abs = filepath.Join(filepath.Dir(pagePath), filepath.FromSlash(ref))
	}
	r, err := filepath.Rel(rootDir, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", ""
	}
	return filepath.ToSlash(r), abs
}

// domLookups are the DOM calls whose first argument names an element: the
// prefix turns the argument into a selector.
var domLookups = map[string]string{
	"getElementById":         "#",
	"getElementsByClassName": ".",
	"getElementsByTagName":   "",
	"querySelector":          "",
	"querySelectorAll":       "",
	"closest":                "",
}

// parseHTMLSource parses a page.
func parseHTMLSource(ctx context.Context, rootDir, filePath string, src []byte) ([]Node, []Edge, string, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(html.GetLanguage())
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil, "", err
	}
	defer tree.Close()
	page := gatherPage(tree.RootNode(), src)

	rel := webRelPath(rootDir, filePath)
	nodes := []Node{{
		FQN:       rel,
		ShortName: strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		Kind:      "package",
		LineStart: 1,
		LineEnd:   int(tree.RootNode().EndPoint().Row) + 1,
	}}
	var edges []Edge

	// Files the page pulls in.
	for _, s := range page.scriptSrcs {
		if r, _ := localRef(rootDir, filePath, s); r != "" {
			edges = append(edges, Edge{SourceFQN: rel, TargetFQN: r, Relation: "imports"})
		}
	}

	// The rules that can style this page: its own <style>s, then the linked
	// stylesheets as they are on disk.
	var rules []cssRule
	for _, body := range page.styles {
		inline := cssRules(ctx, rel, []byte(body.Content(src)), int(body.StartPoint().Row))
		for _, r := range inline {
			nodes = append(nodes, Node{FQN: r.fqn, ShortName: r.selector, Kind: "style", LineStart: r.lineStart, LineEnd: r.lineEnd})
		}
		rules = append(rules, inline...)
	}
	for _, href := range page.stylesheets {
		r, abs := localRef(rootDir, filePath, href)
		if r == "" {
			continue
		}
		edges = append(edges, Edge{SourceFQN: rel, TargetFQN: r, Relation: "imports"})
		// Read only what is inside the workspace once links are resolved: a
		// stylesheet that is a symlink out of it is named, never read.
		real, _, err := fsutil.ResolveInWorkspace(rootDir, abs)
		if err != nil {
			continue
		}
		if data, err := os.ReadFile(real); err == nil {
			rules = append(rules, cssRules(ctx, r, data, 0)...)
		}
	}

	// Elements: every id, every class in use, and the tags a rule styles.
	styledTags := map[string]bool{}
	for _, r := range rules {
		for _, t := range r.targets {
			if t.tag != "" && t.id == "" && len(t.classes) == 0 {
				styledTags[t.tag] = true
			}
		}
	}
	elemNode := map[string]int{} // fqn → index into nodes
	addElem := func(name string, e htmlElement) {
		fqn := rel + "::" + name
		if _, ok := elemNode[fqn]; ok {
			return // a class or a tag is one symbol, where it is first used
		}
		elemNode[fqn] = len(nodes)
		nodes = append(nodes, Node{FQN: fqn, ShortName: name, Kind: "element", LineStart: e.lineStart, LineEnd: e.lineEnd})
	}
	for _, e := range page.elements {
		if e.id != "" {
			addElem("#"+e.id, e)
		}
		for _, c := range e.classes {
			addElem("."+c, e)
		}
		if styledTags[e.tag] {
			addElem("<"+e.tag+">", e)
		}
	}

	// element -styled_by-> rule.
	for _, r := range rules {
		for _, t := range r.targets {
			var names []string
			if t.id != "" {
				names = append(names, "#"+t.id)
			}
			for _, c := range t.classes {
				names = append(names, "."+c)
			}
			if len(names) == 0 && t.tag != "" {
				names = append(names, "<"+t.tag+">")
			}
			for _, n := range names {
				if _, ok := elemNode[rel+"::"+n]; ok {
					edges = append(edges, Edge{SourceFQN: rel + "::" + n, TargetFQN: r.fqn, Relation: "styled_by"})
				}
			}
		}
	}

	// The inline scripts, as JavaScript on the page's lines.
	for _, body := range page.scripts {
		jsNodes, jsEdges := parsePageScript(ctx, rootDir, filePath, rel, []byte(body.Content(src)), int(body.StartPoint().Row), elemNode)
		nodes = append(nodes, jsNodes...)
		edges = append(edges, jsEdges...)
	}
	return nodes, dedupeEdges(edges), "", nil
}

// parsePageScript parses one inline script. Its symbols are the page's
// ("index.html::draw"); calls and DOM lookups become relations.
func parsePageScript(ctx context.Context, rootDir, filePath, rel string, src []byte, lineOffset int, elems map[string]int) ([]Node, []Edge) {
	lang := javascript.GetLanguage()
	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil
	}
	defer tree.Close()
	root := tree.RootNode()
	// ext ".js" picks the JavaScript queries; filePath keeps the page's name
	// in every FQN.
	fc := &fileCtx{ext: ".js", rootDir: rootDir, filePath: filePath, src: src, lang: lang}
	all, edges, _, err := parseGenericFile(ctx, fc, root)
	if err != nil {
		return nil, nil
	}
	var nodes []Node
	for _, n := range all {
		if n.Kind == "package" {
			continue // the page's own node already stands for the file
		}
		nodes = append(nodes, n)
	}

	// DOM lookups, on the script's own lines before they move. bound maps a
	// variable holding a looked-up element to that element's FQNs.
	bound := map[string][]string{}
	walkTree(root, func(n *sitter.Node) {
		if n.Type() != "call_expression" {
			return
		}
		fn := n.ChildByFieldName("function")
		args := n.ChildByFieldName("arguments")
		if fn == nil || args == nil || fn.Type() != "member_expression" {
			return
		}
		prop := fn.ChildByFieldName("property")
		if prop == nil {
			return
		}
		prefix, ok := domLookups[prop.Content(src)]
		if !ok || args.NamedChildCount() == 0 {
			return
		}
		arg := args.NamedChild(0)
		if arg.Type() != "string" {
			return
		}
		lit := strings.Trim(arg.Content(src), "\"'`")
		var names []string
		if prefix != "" {
			names = []string{prefix + lit}
		} else {
			for _, t := range selectorTargets(lit) {
				if t.id != "" {
					names = append(names, "#"+t.id)
				}
				for _, c := range t.classes {
					names = append(names, "."+c)
				}
				if t.id == "" && len(t.classes) == 0 && t.tag != "" {
					names = append(names, "<"+t.tag+">")
				}
			}
		}
		source := findEnclosingFQN(nodes, int(n.StartPoint().Row)+1)
		if source == "" {
			source = rel
		}
		var found []string
		for _, name := range names {
			if _, ok := elems[rel+"::"+name]; ok {
				found = append(found, rel+"::"+name)
				edges = append(edges, Edge{SourceFQN: source, TargetFQN: rel + "::" + name, Relation: "uses"})
			}
		}
		// const canvas = document.getElementById('canvas'): the variable
		// stands for the element from here on.
		if decl := n.Parent(); len(found) > 0 && decl != nil && decl.Type() == "variable_declarator" {
			if name := decl.ChildByFieldName("name"); name != nil && name.Type() == "identifier" {
				bound[name.Content(src)] = append(bound[name.Content(src)], found...)
			}
		}
	})

	// A function that uses such a variable reaches its element. Shadowing is
	// not tracked: a local of the same name links too, which a page rarely has.
	if len(bound) > 0 {
		walkTree(root, func(n *sitter.Node) {
			if n.Type() != "identifier" {
				return
			}
			targets, ok := bound[n.Content(src)]
			if !ok {
				return
			}
			if p := n.Parent(); p != nil && p.Type() == "variable_declarator" && p.ChildByFieldName("name") == n {
				return // the declaration itself
			}
			source := findEnclosingFQN(nodes, int(n.StartPoint().Row)+1)
			if source == "" {
				return // top level: the lookup's own edge already says it
			}
			for _, t := range targets {
				edges = append(edges, Edge{SourceFQN: source, TargetFQN: t, Relation: "uses"})
			}
		})
	}

	for i := range nodes {
		nodes[i].LineStart += lineOffset
		nodes[i].LineEnd += lineOffset
	}
	// The script's imports hang off its package node, which is the page.
	for i := range edges {
		if edges[i].SourceFQN == TsPackageFQN(rootDir, filePath) {
			edges[i].SourceFQN = rel
		}
	}
	return nodes, edges
}

func walkTree(n *sitter.Node, visit func(*sitter.Node)) {
	visit(n)
	for i := 0; i < int(n.NamedChildCount()); i++ {
		walkTree(n.NamedChild(i), visit)
	}
}

func dedupeEdges(edges []Edge) []Edge {
	seen := map[Edge]bool{}
	out := edges[:0]
	for _, e := range edges {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

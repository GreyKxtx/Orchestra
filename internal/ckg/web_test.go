package ckg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func nodeByFQN(nodes []Node, fqn string) (Node, bool) {
	for _, n := range nodes {
		if n.FQN == fqn {
			return n, true
		}
	}
	return Node{}, false
}

func hasEdge(edges []Edge, src, tgt, rel string) bool {
	for _, e := range edges {
		if e.SourceFQN == src && e.TargetFQN == tgt && e.Relation == rel {
			return true
		}
	}
	return false
}

const blackHolePage = `<!doctype html>
<html>
<head>
<style>
#canvas { background: #000; }
.panel button:hover { color: red; }
body { margin: 0; }
</style>
<link rel="stylesheet" href="theme.css">
</head>
<body>
<canvas id="canvas"></canvas>
<div class="panel dark"><button>Reset</button></div>
<script type="text/template">function notCode() {}</script>
<script src="app.js"></script>
<script>
const G = 1;
function draw() {
  const c = document.getElementById('canvas');
  document.querySelector('.panel').focus();
  step();
}
function step() {}
</script>
</body>
</html>
`

// The page's logic lives in its inline script: its functions are symbols of
// the page, on the page's own lines.
func TestParseHTML_InlineScriptSymbols(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "theme.css", ".dark { color: #fff; }\n")
	p := writeFile(t, root, "index.html", blackHolePage)

	nodes, edges, _, err := ParseFile(context.Background(), "", root, p)
	if err != nil {
		t.Fatal(err)
	}
	draw, ok := nodeByFQN(nodes, "index.html::draw")
	if !ok {
		t.Fatalf("draw() from the inline script is not a symbol: %+v", nodes)
	}
	if draw.LineStart != 18 || draw.LineEnd != 22 {
		t.Fatalf("draw() lines = %d-%d, want 18-22 (the page's lines)", draw.LineStart, draw.LineEnd)
	}
	if !hasEdge(edges, "index.html::draw", "step", "calls") {
		t.Fatalf("draw → step call missing: %+v", edges)
	}
	if _, ok := nodeByFQN(nodes, "index.html::notCode"); ok {
		t.Fatal("a template script is not code")
	}
	if !hasEdge(edges, "index.html", "app.js", "imports") {
		t.Fatalf("<script src> must link the page to app.js: %+v", edges)
	}
	if !hasEdge(edges, "index.html", "theme.css", "imports") {
		t.Fatalf("<link rel=stylesheet> must link the page to theme.css: %+v", edges)
	}
}

// Components are the elements a stylesheet or a script can name: ids,
// classes and the tags a rule styles. Each is linked to the rules that style
// it, inline or in a linked stylesheet, and to the functions that reach it.
func TestParseHTML_ElementsStylesAndDOMRefs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "theme.css", ".dark { color: #fff; }\n")
	p := writeFile(t, root, "index.html", blackHolePage)

	nodes, edges, _, err := ParseFile(context.Background(), "", root, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, fqn := range []string{"index.html::#canvas", "index.html::.panel", "index.html::.dark", "index.html::<button>", "index.html::<body>"} {
		n, ok := nodeByFQN(nodes, fqn)
		if !ok || n.Kind != "element" {
			t.Fatalf("element %s missing or not an element: %+v", fqn, n)
		}
	}
	rule, ok := nodeByFQN(nodes, "index.html::css #canvas")
	if !ok || rule.Kind != "style" || rule.LineStart != 5 {
		t.Fatalf("inline rule #canvas = %+v, %v", rule, ok)
	}
	cases := [][3]string{
		{"index.html::#canvas", "index.html::css #canvas", "styled_by"},
		{"index.html::<button>", "index.html::css .panel button:hover", "styled_by"},
		{"index.html::<body>", "index.html::css body", "styled_by"},
		{"index.html::.dark", "theme.css::css .dark", "styled_by"},
		{"index.html::draw", "index.html::#canvas", "uses"},
		{"index.html::draw", "index.html::.panel", "uses"},
	}
	for _, c := range cases {
		if !hasEdge(edges, c[0], c[1], c[2]) {
			t.Errorf("missing %s -%s-> %s; edges: %+v", c[0], c[2], c[1], edges)
		}
	}
	if hasEdge(edges, "index.html::.panel", "index.html::css .panel button:hover", "styled_by") {
		t.Error(".panel button styles the button, not the panel")
	}
}

func TestParseCSS_RulesAreSymbols(t *testing.T) {
	root := t.TempDir()
	p := writeFile(t, root, "styles/site.css", ".a { color: red; }\n.a { margin: 0; }\n@media (max-width: 600px) {\n  .b { display: none; }\n}\n")

	nodes, _, _, err := ParseFile(context.Background(), "", root, p)
	if err != nil {
		t.Fatal(err)
	}
	for fqn, line := range map[string]int{"styles/site.css::css .a": 1, "styles/site.css::css .a~2": 2, "styles/site.css::css .b": 4} {
		n, ok := nodeByFQN(nodes, fqn)
		if !ok || n.Kind != "style" || n.LineStart != line {
			t.Errorf("%s = %+v, %v (want a style at line %d)", fqn, n, ok, line)
		}
	}
	if _, ok := nodeByFQN(nodes, "styles/site.css"); !ok {
		t.Error("a stylesheet is a file of the graph: its package node is missing")
	}
}

func TestIndexable_WebFiles(t *testing.T) {
	for _, ext := range []string{".html", ".htm", ".css", ".js", ".go"} {
		if !Indexable(ext) {
			t.Errorf("%s must be indexed", ext)
		}
	}
	if Indexable(".md") {
		t.Error(".md has no symbols to index")
	}
}

// A page's links to a stylesheet are worked out when the page is parsed; a
// stylesheet that changes alone would leave them pointing at rules that are
// gone. The pass parses the pages that link it again.
func TestUpdateGraph_StylesheetChangeReparsesItsPages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "site.css", ".panel { color: red; }\n")
	writeFile(t, root, "index.html", `<html><head><link rel="stylesheet" href="site.css"></head>
<body><div class="panel"></div><div class="box"></div></body></html>
`)
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	orch := NewOrchestrator(store, root)
	ctx := context.Background()
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	styled := func(element, rule string) bool {
		var n int
		_ = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges e JOIN nodes s ON s.id = e.source_id
			WHERE s.fqn = ? AND e.target_fqn = ? AND e.relation = 'styled_by'`, element, rule).Scan(&n)
		return n > 0
	}
	if !styled("index.html::.panel", "site.css::css .panel") {
		t.Fatal("the panel is not linked to its rule after the first pass")
	}

	writeFile(t, root, "site.css", ".box { color: blue; }\n")
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if !styled("index.html::.box", "site.css::css .box") {
		t.Fatal("the page was not parsed again: the new rule is not linked")
	}
	if styled("index.html::.panel", "site.css::css .panel") {
		t.Fatal("the link to a rule that is gone survived")
	}
}

// The Graph view's detail pane reads a symbol's relations from the outline:
// an element lists the rules that style it, a rule the elements it styles.
func TestBuildFileOutline_CarriesStyleLinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "site.css", ".panel { color: red; }\n")
	writeFile(t, root, "index.html", `<html><head><link rel="stylesheet" href="site.css"></head>
<body><div class="panel"></div></body></html>
`)
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := NewOrchestrator(store, root).UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}

	page, err := BuildFileOutline(ctx, store, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	var panel *OutlineSymbol
	for i := range page.Symbols {
		if page.Symbols[i].Name == ".panel" {
			panel = &page.Symbols[i]
		}
	}
	if panel == nil || len(panel.Links) != 1 {
		t.Fatalf(".panel links = %+v", panel)
	}
	if l := panel.Links[0]; l.Relation != "styled_by" || l.Dir != "out" || l.Name != ".panel" || l.Path != "site.css" || l.Line != 1 {
		t.Fatalf(".panel → rule link = %+v", l)
	}

	sheet, err := BuildFileOutline(ctx, store, "site.css")
	if err != nil {
		t.Fatal(err)
	}
	var rule *OutlineSymbol
	for i := range sheet.Symbols {
		if sheet.Symbols[i].Kind == "style" {
			rule = &sheet.Symbols[i]
		}
	}
	if rule == nil || len(rule.Links) != 1 || rule.Links[0].Dir != "in" || rule.Links[0].Path != "index.html" {
		t.Fatalf("the rule does not list the element it styles: %+v", rule)
	}
}

// Pages look an element up once, at the top of the script, and the functions
// work with the variable: the function is what reaches the element.
func TestParseHTML_FunctionsReachElementsThroughVariables(t *testing.T) {
	root := t.TempDir()
	p := writeFile(t, root, "index.html", `<canvas id="canvas"></canvas>
<script>
const canvas = document.getElementById('canvas');
function resize() {
  canvas.width = 10;
}
function other() {}
</script>
`)
	nodes, edges, _, err := ParseFile(context.Background(), "", root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := nodeByFQN(nodes, "index.html::resize"); !ok {
		t.Fatalf("resize missing: %+v", nodes)
	}
	if !hasEdge(edges, "index.html::resize", "index.html::#canvas", "uses") {
		t.Fatalf("resize uses the canvas through its variable: %+v", edges)
	}
	if hasEdge(edges, "index.html::other", "index.html::#canvas", "uses") {
		t.Fatal("other() never touches the canvas")
	}
}

// A linked stylesheet is read only from inside the workspace: one that is a
// symlink out of it is linked by name and never read.
func TestParseHTML_LinkedStylesheetOutsideWorkspaceIsNotRead(t *testing.T) {
	root := t.TempDir()
	outside := writeFile(t, t.TempDir(), "secret.css", ".leak { color: red; }\n")
	if err := os.Symlink(outside, filepath.Join(root, "theme.css")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	p := writeFile(t, root, "index.html", `<link rel="stylesheet" href="theme.css"><div class="leak"></div>`+"\n")
	_, edges, _, err := ParseFile(context.Background(), "", root, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edges {
		if e.Relation == "styled_by" {
			t.Fatalf("a stylesheet outside the workspace was read: %+v", e)
		}
	}
}

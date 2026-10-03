package ckg

import (
	"context"
	"path/filepath"
	"testing"
)

const appCSS = `.app { display: flex; }
.card .title { font-weight: bold; }
.active { color: red; }
#root { margin: 0; }
`

const appTSX = `import React from 'react';
import clsx from 'clsx';
import './App.css';

export function App({ on }: { on: boolean }) {
  return (
    <div id="root" className="app">
      <div className={clsx("card", on && "active")}>
        <h1 className={` + "`title`" + `}>Hi</h1>
      </div>
    </div>
  );
}
`

// A React component styles its elements through className and the stylesheet
// it imports: the graph links each class to the rules that style it, the way
// it does for a page's class attribute.
func TestParseJSX_ClassNamesAreStyledByTheImportedSheet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/App.css", appCSS)
	file := writeFile(t, root, "src/App.tsx", appTSX)

	nodes, edges, _, err := ParseFile(context.Background(), "", root, file)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(edges, "src/App.tsx", "src/App.css", "imports") {
		t.Errorf("missing src/App.tsx -imports-> src/App.css; edges: %+v", edges)
	}
	for _, c := range [][2]string{
		{"src/App.tsx::.app", "src/App.css::css .app"},
		{"src/App.tsx::.title", "src/App.css::css .card .title"},
		{"src/App.tsx::.active", "src/App.css::css .active"},
		{"src/App.tsx::#root", "src/App.css::css #root"},
	} {
		if _, ok := nodeByFQN(nodes, c[0]); !ok {
			t.Errorf("no element %s", c[0])
		}
		if !hasEdge(edges, c[0], c[1], "styled_by") {
			t.Errorf("missing %s -styled_by-> %s", c[0], c[1])
		}
	}
	// The component's own symbol is still there.
	if _, ok := nodeByFQN(nodes, "src/App.tsx::App"); !ok {
		t.Errorf("the component symbol is gone: %+v", nodes)
	}
	// A string that is not a class name is not an element.
	if _, ok := nodeByFQN(nodes, "src/App.tsx::.react"); ok {
		t.Error("an import path was taken for a class")
	}
}

// The component's links are worked out when it is parsed, so a change to the
// stylesheet alone has to parse it again — as it does a page.
func TestUpdateGraph_StylesheetChangeReparsesItsComponents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/App.css", ".panel { color: red; }\n")
	writeFile(t, root, "src/App.jsx", "import './App.css';\nexport const App = () => <div><p className=\"panel\"/><p className=\"box\"/></div>;\n")
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
	if !styled("src/App.jsx::.panel", "src/App.css::css .panel") {
		t.Fatal("the panel is not linked to its rule after the first pass")
	}
	writeFile(t, root, "src/App.css", ".box { color: blue; }\n")
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if !styled("src/App.jsx::.box", "src/App.css::css .box") {
		t.Fatal("the component was not parsed again: the new rule is not linked")
	}
	if styled("src/App.jsx::.panel", "src/App.css::css .panel") {
		t.Fatal("the link to a rule that is gone survived")
	}
}

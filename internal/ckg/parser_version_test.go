package ckg

import (
	"context"
	"path/filepath"
	"testing"
)

// A graph indexed by an older parser keeps its files' stamps, so an unchanged
// file is never parsed again and what the new parser finds in it (pages,
// stylesheets, React classNames) never appears. A parser version that moved
// makes every file be parsed once more on the next pass.
func TestStore_ANewParserParsesEveryFileAgain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/App.css", ".app { color: red; }\n")
	writeFile(t, root, "src/App.jsx", "import './App.css';\nexport const App = () => <div className=\"app\"/>;\n")
	dbPath := filepath.Join(t.TempDir(), "ckg.db")
	ctx := context.Background()

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewOrchestrator(store, root).UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	// What an older binary left behind: no styled_by edge, and the version
	// of the parser that made the graph.
	if _, err := store.db.Exec(`DELETE FROM edges WHERE relation = 'styled_by'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE ckg_meta SET value = '0' WHERE key = 'parser_version'`); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	store, err = NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := NewOrchestrator(store, root).UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges WHERE relation = 'styled_by'`).Scan(&n)
	if n == 0 {
		t.Fatal("the unchanged component was not parsed again under the new parser")
	}
}

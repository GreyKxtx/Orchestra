package ckg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The refresh on an unchanged tree (DATA-3). Every pass used to hash every
// file and re-resolve every dangling edge with up to three queries each: 3 s
// on Orchestra's own tree, paid by every explore and every agent run.

func writeGoFile(t *testing.T, root, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newIndexedTree(t *testing.T) (string, *Store, *Orchestrator) {
	t.Helper()
	root := t.TempDir()
	writeAppModule(t, root)
	// Beta calls Gamma, which no file defines yet: a dangling edge.
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n")
	writeGoFile(t, root, "b.go", "package app\n\nfunc Beta() { Alpha(); Gamma() }\n")
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	orch := NewOrchestrator(store, root)
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	return root, store, orch
}

func TestScanChanges_UnchangedFilesAreNotHashed(t *testing.T) {
	root, _, orch := newIndexedTree(t)
	ctx := context.Background()
	res, err := orch.scanner.ScanChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hashed != 0 || len(res.ToParse) != 0 || len(res.ToDelete) != 0 {
		t.Fatalf("an unchanged tree is known by its stamps: hashed=%d parse=%v delete=%v", res.Hashed, res.ToParse, res.ToDelete)
	}

	// A touch: the same bytes with a new mtime are hashed once, found
	// unchanged, and restamped so the next walk does not read them.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "a.go"), future, future); err != nil {
		t.Fatal(err)
	}
	res, err = orch.scanner.ScanChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hashed != 1 || len(res.ToParse) != 0 || len(res.Restamp) != 1 {
		t.Fatalf("a touched file is hashed and restamped, not parsed: hashed=%d parse=%v restamp=%v", res.Hashed, res.ToParse, res.Restamp)
	}
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	res, err = orch.scanner.ScanChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hashed != 0 {
		t.Fatalf("after the restamp the touched file is known again: hashed=%d", res.Hashed)
	}

	// A real edit is parsed.
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n\nfunc Alpha2() {}\n")
	res, err = orch.scanner.ScanChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToParse) != 1 || res.ToParse[0] != "a.go" {
		t.Fatalf("an edited file is parsed: %v", res.ToParse)
	}
}

func TestFileStamps_SurviveReopen(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n")
	dbPath := filepath.Join(t.TempDir(), "ckg.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewOrchestrator(store, root).UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	res, err := NewScanner(store, root).ScanChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hashed != 0 {
		t.Fatalf("stamps are in the database, not in the process: hashed=%d", res.Hashed)
	}
}

// A files table from before the stamp columns gains them on open and keeps
// its rows; those rows are hashed once more and restamped.
func TestEnsureFileStamps_OldDatabaseGainsTheColumns(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n")
	dbPath := filepath.Join(t.TempDir(), "ckg.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewOrchestrator(store, root).UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"mtime_ns", "size"} {
		if _, err := store.db.Exec("ALTER TABLE files DROP COLUMN " + col); err != nil {
			t.Fatalf("drop %s: %v", col, err)
		}
	}
	_ = store.Close()

	store, err = NewStore(dbPath)
	if err != nil {
		t.Fatalf("open a database without stamp columns: %v", err)
	}
	defer store.Close()
	for _, col := range []string{"mtime_ns", "size"} {
		if has, err := store.columnExists("files", col); err != nil || !has {
			t.Fatalf("files.%s after reopen: has=%v err=%v", col, has, err)
		}
	}
	files, err := store.GetAllFiles(context.Background())
	if err != nil || len(files) != 1 {
		t.Fatalf("the graph survives the column add: %v %v", files, err)
	}
	res, err := NewScanner(store, root).ScanChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hashed != 1 || len(res.ToParse) != 0 || len(res.Restamp) != 1 {
		t.Fatalf("a row with a zero stamp is hashed once and restamped: hashed=%d parse=%v restamp=%v", res.Hashed, res.ToParse, res.Restamp)
	}
}

func TestUpdateGraph_EmptyPassLooksAtNoEdge(t *testing.T) {
	root, store, orch := newIndexedTree(t)
	ctx := context.Background()
	if first := store.LastRefresh(); first.Parsed != 2 || first.Hashed != 2 {
		t.Fatalf("the first pass hashes and parses both files: %+v", first)
	}
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	st := store.LastRefresh()
	if st.Hashed != 0 || st.Parsed != 0 || st.Deleted != 0 || st.Candidates != 0 || st.Locked != 0 {
		t.Fatalf("an unchanged tree costs a walk and nothing else: %+v", st)
	}

	// A method call on a variable is recorded by its short name ("Run") and
	// resolved by the pass that indexes the one method of that name in the
	// package. The pass parses one file and looks at the edges that name
	// its symbols — not the whole graph.
	writeGoFile(t, root, "b.go", "package app\n\nfunc Beta(a *Agent) { Alpha(); Gamma(); a.Run() }\n")
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, root, "c.go", "package app\n\ntype Agent struct{}\n\nfunc (a *Agent) Run() {}\n")
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	st = store.LastRefresh()
	if st.Parsed != 1 || st.Relinked != 1 || st.Candidates != 1 {
		t.Fatalf("one file, one edge: %+v\n%s", st, dumpEdges(t, store))
	}
	var targetID *int64
	var targetFQN string
	if err := store.db.QueryRow(`
		SELECT e.target_id, e.target_fqn FROM edges e JOIN nodes src ON src.id = e.source_id
		WHERE src.short_name = 'Beta' AND e.relation = 'calls' AND e.target_fqn LIKE '%Run'`).Scan(&targetID, &targetFQN); err != nil {
		t.Fatalf("%v\n%s", err, dumpEdges(t, store))
	}
	if targetID == nil || !strings.HasSuffix(targetFQN, "Agent.Run") {
		t.Fatalf("Beta→Run must point at Agent.Run: id=%v fqn=%q", targetID, targetFQN)
	}
	// The edge to Gamma, which nothing defines, was not a candidate and is
	// still dangling.
	var gammaID *int64
	if err := store.db.QueryRow(`
		SELECT e.target_id FROM edges e JOIN nodes src ON src.id = e.source_id
		WHERE src.short_name = 'Beta' AND e.relation = 'calls' AND e.target_fqn LIKE '%Gamma'`).Scan(&gammaID); err != nil {
		t.Fatalf("%v\n%s", err, dumpEdges(t, store))
	}
	if gammaID != nil {
		t.Fatalf("Gamma is not defined anywhere, yet the edge points at node %d", *gammaID)
	}
}

func dumpEdges(t *testing.T, store *Store) string {
	t.Helper()
	rows, err := store.db.Query(`SELECT src.short_name, e.target_fqn, e.relation, e.target_id, e.is_external FROM edges e JOIN nodes src ON src.id = e.source_id ORDER BY src.short_name, e.target_fqn`)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var src, tgt, rel string
		var tid *int64
		var ext int
		if err := rows.Scan(&src, &tgt, &rel, &tid, &ext); err != nil {
			return err.Error()
		}
		resolved := "dangling"
		if tid != nil {
			resolved = fmt.Sprintf("→%d", *tid)
		}
		fmt.Fprintf(&b, "  %s -%s-> %s (%s, external=%d)\n", src, rel, tgt, resolved, ext)
	}
	return b.String()
}

func TestNewStore_FileDatabaseUsesWAL(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal: the TUI, the extension and ckg-ui read while a refresh writes", mode)
	}
}

func TestRefreshInBackground_CoalescesAndCompletes(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n")
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	orch := NewOrchestrator(store, root)
	ctx := context.Background()
	orch.RefreshInBackground(ctx)
	orch.RefreshInBackground(ctx) // a second call while one runs starts nothing
	deadline := time.Now().Add(20 * time.Second)
	for {
		files, err := store.GetAllFiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 1 && !store.refreshing.Load() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background pass did not index the tree: files=%v refreshing=%v", files, store.refreshing.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := store.LastRefresh(); st.Parsed != 1 {
		t.Fatalf("one pass, one file: %+v", st)
	}
}

// The warmup reserves both locks before its goroutine runs; the pass must
// not take them again. It deadlocked once: the scanner read the stamps
// under the read lock while the warmup held the write lock.
func TestUpdateGraphAsync_CompletesAndReleasesTheLocks(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	writeGoFile(t, root, "a.go", "package app\n\nfunc Alpha() {}\n")
	store, err := NewStore(filepath.Join(t.TempDir(), "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	orch := NewOrchestrator(store, root)
	ctx := context.Background()
	select {
	case err := <-orch.UpdateGraphAsync(ctx):
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the warmup pass never finished: it waits on a lock it holds")
	}
	done := make(chan error, 1)
	go func() { done <- orch.UpdateGraph(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the locks were not released after the warmup")
	}
	if _, err := store.TraverseBFS(ctx, funcFQN(t, store, "Alpha"), DirectionDownstream, TraversalOptions{MaxDepth: 1, MaxNodes: 5}); err != nil {
		t.Fatal(err)
	}
}

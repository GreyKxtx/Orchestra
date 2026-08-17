package ckg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOrchestrator(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ckg_orch_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	src := `package main
	
type Animal struct {}
func (a *Animal) Speak() {}
`
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	// Use unique memory DB for this test to avoid collision
	store, err := NewStore("file:ckgorch?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	orch := NewOrchestrator(store, tempDir)
	ctx := context.Background()

	// 1. Initial UpdateGraph
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatalf("UpdateGraph failed: %v", err)
	}

	// Verify DB state
	files, err := store.GetAllFiles(ctx)
	if err != nil {
		t.Fatalf("GetAllFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 file in DB, got %d", len(files))
	}

	// 2. Modify existing file and add a new one
	srcModified := src + "func NewAnimal() *Animal { return nil }\n"
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(srcModified), 0644); err != nil {
		t.Fatalf("failed to modify dummy file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "b.go"), []byte("package main\nfunc foo() {}"), 0644); err != nil {
		t.Fatalf("failed to write new dummy file: %v", err)
	}

	// 3. Incremental UpdateGraph
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatalf("UpdateGraph failed: %v", err)
	}

	files, err = store.GetAllFiles(ctx)
	if err != nil {
		t.Fatalf("GetAllFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("Expected 2 files in DB, got %d", len(files))
	}
}

func TestOrchestratorIndexesReactSourcesWithDistinctLanguages(t *testing.T) {
	root := t.TempDir()
	sources := map[string]string{
		"App.jsx":  `export function App() { return <main>Hello</main> }`,
		"main.tsx": `const Main = (): JSX.Element => <App />; export default Main;`,
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := NewOrchestrator(store, root).UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := store.IndexStats(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Fatalf("indexed files = %d, want 2", stats.Files)
	}
	if stats.Langs["jsx"] != 1 || stats.Langs["tsx"] != 1 {
		t.Fatalf("language distribution = %#v, want jsx=1 and tsx=1", stats.Langs)
	}
}

func writeAppModule(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func funcFQN(t *testing.T, s *Store, short string) string {
	t.Helper()
	var fqn string
	err := s.db.QueryRow(`SELECT fqn FROM nodes WHERE short_name = ? AND kind = 'func'`, short).Scan(&fqn)
	if err != nil {
		t.Fatalf("lookup %s: %v", short, err)
	}
	return fqn
}

func countCallEdges(t *testing.T, s *Store, srcShort, tgtShort string) int {
	t.Helper()
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM edges e
		JOIN nodes src ON src.id = e.source_id
		LEFT JOIN nodes tgt ON tgt.id = e.target_id
		WHERE e.relation = 'calls'
		  AND src.short_name = ?
		  AND (tgt.short_name = ? OR e.target_fqn LIKE '%' || ?)`,
		srcShort, tgtShort, tgtShort).Scan(&n)
	if err != nil {
		t.Fatalf("count edges: %v", err)
	}
	return n
}

func TestUpdateGraph_RemovesStaleCallEdge(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package app\n\nfunc Alpha() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package app\n\nfunc Beta() { Alpha() }\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
	if countCallEdges(t, store, "Beta", "Alpha") == 0 {
		t.Fatal("expected Beta→Alpha call edge after initial index")
	}

	beta := funcFQN(t, store, "Beta")
	sg, err := store.TraverseBFS(ctx, beta, DirectionDownstream, TraversalOptions{MaxDepth: 2, MaxNodes: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range sg.Edges {
		if e.Relation == "calls" && strings.Contains(e.TargetFQN, "Alpha") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("TraverseBFS missing Alpha callee: edges=%+v", sg.Edges)
	}

	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package app\n\nfunc Beta() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if countCallEdges(t, store, "Beta", "Alpha") != 0 {
		t.Fatal("zombie edge Beta→Alpha survived after the call was removed")
	}
	beta = funcFQN(t, store, "Beta")
	sg, err = store.TraverseBFS(ctx, beta, DirectionDownstream, TraversalOptions{MaxDepth: 2, MaxNodes: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sg.Edges {
		if e.Relation == "calls" && strings.Contains(e.TargetFQN, "Alpha") {
			t.Fatalf("TraverseBFS still has stale Alpha edge: %+v", e)
		}
	}
}

func TestUpdateGraph_RelinksNewCallToExistingNode(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package app\n\nfunc Alpha() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package app\n\nfunc Beta() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
	if countCallEdges(t, store, "Beta", "Alpha") != 0 {
		t.Fatal("no call should exist yet")
	}

	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package app\n\nfunc Beta() { Alpha() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := orch.UpdateGraph(ctx); err != nil {
		t.Fatal(err)
	}

	var targetID *int64
	err = store.db.QueryRow(`
		SELECT e.target_id FROM edges e
		JOIN nodes src ON src.id = e.source_id
		WHERE src.short_name = 'Beta' AND e.relation = 'calls' AND e.target_fqn LIKE '%Alpha'`).Scan(&targetID)
	if err != nil {
		t.Fatalf("relinked edge: %v", err)
	}
	if targetID == nil {
		t.Fatal("new Beta→Alpha edge was not relinked to Alpha's node id")
	}
}

func TestTraverseBFS_ConcurrentWithUpdateGraph(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	srcA := "package app\n\nfunc Alpha() {}\n"
	srcB := "package app\n\nfunc Beta() { Alpha() }\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(srcA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte(srcB), 0644); err != nil {
		t.Fatal(err)
	}

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
	beta := funcFQN(t, store, "Beta")

	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 12; j++ {
				if _, err := store.TraverseBFS(ctx, beta, DirectionBoth, TraversalOptions{MaxDepth: 2, MaxNodes: 20}); err != nil {
					errCh <- err
					return
				}
				nodes, err := store.FindRelevantNodes(ctx, "Alpha Beta", 5)
				if err != nil {
					errCh <- err
					return
				}
				_ = store.FormatPromptContext(ctx, nodes, 200)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 8; j++ {
			comment := fmt.Sprintf("package app\n\n// tick %d\nfunc Alpha() {}\n", j)
			if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(comment), 0644); err != nil {
				errCh <- err
				return
			}
			if err := orch.UpdateGraph(ctx); err != nil {
				errCh <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

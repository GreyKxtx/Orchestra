package ckg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScannerIncremental(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "ckg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create 10 dummy .go files
	for i := 0; i < 10; i++ {
		filePath := filepath.Join(tempDir, "file_"+string(rune('A'+i))+".go")
		if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {}"), 0644); err != nil {
			t.Fatalf("failed to write dummy file: %v", err)
		}
	}

	// Create an in-memory SQLite store
	// using unique db name for shared memory to avoid test collisions
	store, err := NewStore("file:ckgtest?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	scanner := NewScanner(store, tempDir)
	ctx := context.Background()

	// 1. Initial scan
	start := time.Now()
	toParse, toDelete, err := scanner.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	elapsed1 := time.Since(start)

	if len(toParse) != 10 {
		t.Errorf("Expected 10 files to parse, got %d", len(toParse))
	}
	if len(toDelete) != 0 {
		t.Errorf("Expected 0 files to delete, got %d", len(toDelete))
	}

	// Mocking AST parser success by saving them to store
	for _, path := range toParse {
		fullPath := filepath.Join(tempDir, path)
		hash, _ := hashFile(fullPath)
		if err := store.SaveFileNodes(ctx, path, hash, "go", "", "main", nil, nil); err != nil {
			t.Fatalf("SaveFileNodes failed: %v", err)
		}
	}

	// 2. Incremental scan (no changes)
	start = time.Now()
	toParse, toDelete, err = scanner.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	elapsed2 := time.Since(start)

	if len(toParse) != 0 {
		t.Errorf("Expected 0 files to parse on second scan, got %d", len(toParse))
	}
	if len(toDelete) != 0 {
		t.Errorf("Expected 0 files to delete on second scan, got %d", len(toDelete))
	}

	// 3. Modify one file and delete another
	if err := os.WriteFile(filepath.Join(tempDir, "file_A.go"), []byte("package main\n\nfunc main() { fmt.Println(1) }"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}
	if err := os.Remove(filepath.Join(tempDir, "file_B.go")); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	// 4. Scan after modifications
	start = time.Now()
	toParse, toDelete, err = scanner.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	elapsed3 := time.Since(start)

	if len(toParse) != 1 || toParse[0] != "file_A.go" {
		t.Errorf("Expected toParse to contain only file_A.go, got: %v", toParse)
	}
	if len(toDelete) != 1 || toDelete[0] != "file_B.go" {
		t.Errorf("Expected toDelete to contain only file_B.go, got: %v", toDelete)
	}

	t.Logf("Initial scan: %v, Unchanged scan: %v, Modified scan: %v", elapsed1, elapsed2, elapsed3)
}

func TestScannerIncludesReactSourcesAndAllParserLanguages(t *testing.T) {
	tempDir := t.TempDir()
	files := map[string]string{
		"src/App.jsx":       "export function App() { return <main /> }\n",
		"src/main.tsx":      "export const Main = () => <App />\n",
		"src/util.ts":       "export const value = 1\n",
		"src/legacy.JS":     "export function legacy() {}\n",
		"src/styles.css":    "main { display: block; }\n",
		"public/index.html": "<main></main>\n",
	}
	for rel, content := range files {
		path := filepath.Join(tempDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	toParse, _, err := NewScanner(store, tempDir).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(toParse))
	for _, path := range toParse {
		got[path] = true
	}
	for _, want := range []string{"src/App.jsx", "src/main.tsx", "src/util.ts", "src/legacy.JS"} {
		if !got[want] {
			t.Errorf("supported source %q was not discovered: %v", want, toParse)
		}
	}
	for _, unwanted := range []string{"src/styles.css", "public/index.html"} {
		if got[unwanted] {
			t.Errorf("non-AST source %q should not be indexed", unwanted)
		}
	}
}

func TestScannerRespectsConfiguredIgnores(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"src/app.ts", "generated/api/client.ts", "web/cache/runtime.js"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("export const value = 1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	toParse, _, err := NewScannerWithIgnores(
		store,
		root,
		[]string{"generated/api", "cache"},
	).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(toParse) != 1 || toParse[0] != "src/app.ts" {
		t.Fatalf("discovered files = %v, want only src/app.ts", toParse)
	}
}

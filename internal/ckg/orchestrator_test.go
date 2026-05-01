package ckg

import (
	"context"
	"os"
	"path/filepath"
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

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// explore in a turn answers from the model's staged edits, not the disk:
// a function the model just wrote is in the graph it explores, and the disk
// file is as it was (LLM-11).
func TestExplore_SeesTheStagedEdit(t *testing.T) {
	root := t.TempDir()
	const disk = "package app\n\nfunc Alpha() {}\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(disk), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	ctx := context.Background()
	hash := r.overlayAt(ctx).CurrentHash("a.go")
	if _, err := r.FSWrite(ctx, FSWriteRequest{Path: "a.go", Content: "package app\n\nfunc Alpha() { Delta() }\n\nfunc Delta() {}\n", FileHash: hash}); err != nil {
		t.Fatal(err)
	}
	resp, err := r.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: "Delta"})
	if err != nil {
		t.Fatalf("explore in a turn with a staged Delta: %v", err)
	}
	if !strings.Contains(resp.Content, "Delta") || !strings.Contains(resp.Content, "a.go") {
		t.Fatalf("explore answers from the staged file:\n%s", resp.Content)
	}
	outline, ok, err := r.CKGFileOutline(ctx, "a.go")
	if err != nil || !ok || outline == nil {
		t.Fatalf("outline: %v %v", ok, err)
	}
	names := []string{}
	for _, s := range outline.Symbols {
		names = append(names, s.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "Delta") {
		t.Fatalf("the outline lists the staged symbols: %v", names)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.go")); string(b) != disk {
		t.Fatalf("the disk changed under a preview: %q", b)
	}
	// Once the edit is dropped the graph is the disk's again.
	r.TurnAt(ctx).ClearStaged()
	resp, err = r.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: "Delta"})
	if err == nil && strings.Contains(resp.Content, "func Delta") {
		t.Fatalf("a dropped edit is not in the graph:\n%s", resp.Content)
	}
}

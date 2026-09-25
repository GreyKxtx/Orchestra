package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// rg runs under the caller's context: a cancelled turn kills it instead of
// letting it finish the tree for an answer nobody reads.
func TestSearchWithRipgrepContext_CancelledContextStops(t *testing.T) {
	if !HasRipgrep() {
		t.Skip("rg not installed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := SearchWithRipgrepContext(ctx, root, "needle", nil, DefaultOptions(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	// And an intact context still answers.
	m, err := SearchWithRipgrepContext(context.Background(), root, "needle", nil, DefaultOptions(), nil)
	if err != nil || len(m) != 1 {
		t.Fatalf("live search: %v %v", m, err)
	}
}

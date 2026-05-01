package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerOpensCKGStoreOnce(t *testing.T) {
	tmp := t.TempDir()

	// Create a minimal Go file so there is something for the parser to find.
	src := "package foo\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(tmp, "foo.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner, err := NewRunner(tmp, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { runner.Close() })

	if runner.ckgStore == nil {
		t.Fatal("ckgStore is nil after NewRunner")
	}
	storePtr := runner.ckgStore

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := runner.ExploreCodebase(ctx, ExploreCodebaseRequest{SymbolName: "Hello"}); err != nil {
			t.Fatalf("ExploreCodebase #%d: %v", i, err)
		}
		if runner.ckgStore != storePtr {
			t.Fatalf("ckgStore pointer changed on call #%d — store reopened!", i)
		}
	}
}

func TestRunnerCloseIdempotent(t *testing.T) {
	tmp := t.TempDir()
	runner, err := NewRunner(tmp, RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

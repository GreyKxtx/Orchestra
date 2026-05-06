package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCodeSymbols_Go_RegexFallback(t *testing.T) {
	root := t.TempDir()
	src := `package main

type Foo struct{}

func Bar() {}

func (f Foo) Baz() {}
`
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	resp, err := r.CodeSymbols(context.Background(), CodeSymbolsRequest{Path: "a.go"})
	if err != nil {
		t.Fatalf("CodeSymbols failed: %v", err)
	}

	got := make(map[string]bool)
	for _, s := range resp.Symbols {
		got[s.Name] = true
	}
	for _, want := range []string{"Foo", "Bar", "Baz"} {
		if !got[want] {
			t.Fatalf("expected symbol %q, got %+v", want, resp.Symbols)
		}
	}
}

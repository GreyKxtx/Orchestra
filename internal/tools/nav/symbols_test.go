package nav

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lsp"
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

	c := NewClient(root, nil, config.EmbedConfig{}, nil, func() *lsp.Manager { return nil })

	resp, err := c.CodeSymbols(context.Background(), CodeSymbolsRequest{Path: "a.go"})
	if err != nil {
		t.Fatalf("CodeSymbols failed: %v", err)
	}

	got := make(map[string]string)
	for _, s := range resp.Symbols {
		got[s.Name] = s.Kind
	}
	want := map[string]string{
		"Foo": "type",
		"Bar": "function",
		"Baz": "method",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Fatalf("symbol %q: want kind %q, got %q (all: %+v)", name, kind, got[name], resp.Symbols)
		}
	}
}

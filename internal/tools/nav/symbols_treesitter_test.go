//go:build cgo

package nav

import (
	"context"
	"testing"
)

func TestGoSymbolsViaTreeSitter_GoFile(t *testing.T) {
	src := []byte(`package main

type Foo struct{}

func Bar() {}

func (f Foo) Baz() {}
`)
	syms, ok := goSymbolsViaTreeSitter(context.Background(), src)
	if !ok {
		t.Fatal("expected tree-sitter parse to succeed with CGO")
	}
	got := make(map[string]string)
	for _, s := range syms {
		got[s.Name] = s.Kind
	}
	want := map[string]string{
		"Foo": "type",
		"Bar": "function",
		"Baz": "method",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Fatalf("symbol %q: want kind %q, got %q (all: %+v)", name, kind, got[name], syms)
		}
	}
}

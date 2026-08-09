package resolver_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/patch/resolver"
)

func TestApplySearchReplaceWithScope_UniqueInFunction(t *testing.T) {
	content := []byte("package main\n\nfunc A() {\n\tif err != nil {}\n}\n\nfunc B() {\n\tif err != nil {}\n}\n")
	search := "if err != nil {}"
	replace := "if err != nil { return err }"

	_, err := resolver.ApplySearchReplace(content, search, replace)
	if err == nil {
		t.Fatal("expected ambiguous on full file")
	}

	got, err := resolver.ApplySearchReplaceWithScope(content, search, replace, 3, 5)
	if err != nil {
		t.Fatalf("scoped apply: %v", err)
	}
	if strings.Count(string(got), "return err") != 1 {
		t.Fatalf("expected one scoped fix, got %q", got)
	}
}

func TestApplySearchReplaceWithScope_AmbiguousInsideScope(t *testing.T) {
	content := []byte("func X() {\n\ta()\n\ta()\n}\n")
	_, err := resolver.ApplySearchReplaceWithScope(content, "a()", "b()", 1, 4)
	if err == nil {
		t.Fatal("expected ambiguous inside scope")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.AmbiguousMatch {
		t.Fatalf("got %v", err)
	}
}

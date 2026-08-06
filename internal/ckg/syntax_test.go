package ckg

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

func TestValidateSyntax_ValidGo(t *testing.T) {
	src := []byte("package main\n\nfunc main() {}\n")
	if err := ValidateSyntax("main.go", src); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidateSyntax_InvalidGo_MissingBrace(t *testing.T) {
	src := []byte("package main\n\nfunc main() {\n")
	err := ValidateSyntax("broken.go", src)
	if err == nil {
		t.Fatal("expected syntax error")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.SyntaxError {
		t.Fatalf("want SyntaxError, got %v", err)
	}
	if !strings.Contains(pe.Message, "line") {
		t.Fatalf("message should mention line: %q", pe.Message)
	}
}

func TestValidateSyntax_UnknownExtensionSkipped(t *testing.T) {
	if err := ValidateSyntax("readme.md", []byte("# not code\n")); err != nil {
		t.Fatalf("markdown should skip gate: %v", err)
	}
}

func TestValidateSyntax_EmptyContentSkipped(t *testing.T) {
	if err := ValidateSyntax("main.go", nil); err != nil {
		t.Fatalf("empty should skip: %v", err)
	}
}

func TestValidateSyntax_ValidPython(t *testing.T) {
	src := []byte("def hello():\n    return 1\n")
	if err := ValidateSyntax("hello.py", src); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidateSyntax_InvalidPython(t *testing.T) {
	src := []byte("def hello(\n")
	err := ValidateSyntax("bad.py", src)
	if err == nil {
		t.Fatal("expected syntax error")
	}
}

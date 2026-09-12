package repomap

import (
	"context"
	"strings"
	"testing"
)

// A budget far too small for per-file outlines must still answer "what is this
// project": the directories and how much is in each. Dropping the smallest
// files first keeps one big outline and omits everything else, which answers a
// question nobody asked.
func TestFormat_TinyBudget_AnswersWithTheShapeOfTheRepo(t *testing.T) {
	root := t.TempDir()
	var big strings.Builder
	big.WriteString("package huge\n")
	for i := 0; i < 60; i++ {
		big.WriteString("func HugeSymbolNumber" + string(rune('A'+i%26)) + string(rune('a'+i%26)) + "() {}\n")
	}
	writeFile(t, root, "internal/huge/huge.go", big.String())
	writeFile(t, root, "internal/alpha/alpha.go", "package alpha\nfunc Alpha() {}\n")
	writeFile(t, root, "cmd/tool/main.go", "package main\nfunc main() {}\n")

	rm, _ := Build(context.Background(), root, Options{})
	out := Format(rm, 300)

	if len(out) > 300 {
		t.Fatalf("the budget must hold: %d bytes\n%s", len(out), out)
	}
	for _, dir := range []string{"internal/huge", "internal/alpha", "cmd/tool"} {
		if !strings.Contains(out, dir) {
			t.Fatalf("every directory must be named, %q is missing:\n%s", dir, out)
		}
	}
	if strings.Contains(out, "HugeSymbolNumber") {
		t.Fatalf("a tiny budget must not be spent on one file's symbols:\n%s", out)
	}
	if !strings.Contains(out, "files") || !strings.Contains(out, "symbols") {
		t.Fatalf("the digest must say how much is where:\n%s", out)
	}
}

// A budget that can hold most of the outline still gets the outline.
func TestFormat_RoomyBudget_KeepsTheOutline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package a\nfunc Alpha() {}\n")
	writeFile(t, root, "b.go", "package b\nfunc Beta() {}\n")
	rm, _ := Build(context.Background(), root, Options{})
	full := Format(rm, 0)
	out := Format(rm, len(full))
	if !strings.Contains(out, "func Alpha") || !strings.Contains(out, "func Beta") {
		t.Fatalf("an outline that fits must not be replaced by a digest:\n%s", out)
	}
}

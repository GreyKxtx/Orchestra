package fs

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/patch/patches"
)

// The patch that destroyed two files in an eval run, verbatim off the wire:
// type file.write_atomic, the search/replace fields filled in, content empty.
//
// Applied as written it puts "" in the file. The tools had already written the
// right content to disk, so the turn finished by erasing its own work and
// reporting success.
const realUtilGo = "package main\n"

func TestWriteAtomicRefusal_TypeAndFieldsDisagree(t *testing.T) {
	p := patches.Patch{
		Type:    patches.TypeFileWriteAtomic,
		Path:    "util.go",
		Search:  "",
		Replace: "package main\n\nfunc Double(n int) int {\n\treturn n * 2\n}\n",
	}

	got := writeAtomicPatchRefusal(p, realUtilGo)
	if got == "" {
		t.Fatal("a write_atomic carrying replace and no content was accepted — it writes an empty file")
	}
	// The model has to learn WHICH mistake it made. "content is empty" would
	// send it to fill in content when what it wanted was a partial edit.
	if !strings.Contains(got, "file.search_replace") {
		t.Errorf("the refusal must name the type the model actually wanted:\n%s", got)
	}
	if !strings.Contains(got, "content") {
		t.Errorf("the refusal must name the field a whole-file write needs:\n%s", got)
	}
}

// The guard the resolver path has always had, now on this path too: a
// whole-file write must not replace a real file with nothing.
func TestWriteAtomicRefusal_EmptyContentOverAFileIsRefused(t *testing.T) {
	p := patches.Patch{Type: patches.TypeFileWriteAtomic, Path: "util.go", Content: ""}

	if got := writeAtomicPatchRefusal(p, "package main\n\nfunc Double(n int) int {\n\treturn n * 2\n}\n"); got == "" {
		t.Error("writing an empty file over a real one was accepted")
	}
}

// And the refusals must stop there. A whole-file write is the normal way to
// create a file and to rewrite one, and a guard that blocks those blocks the
// tool.
func TestWriteAtomicRefusal_AnOrdinaryWholeFileWriteIsAllowed(t *testing.T) {
	p := patches.Patch{
		Type:    patches.TypeFileWriteAtomic,
		Path:    "util.go",
		Content: "package main\n\nfunc Double(n int) int {\n\treturn n * 2\n}\n",
	}

	if got := writeAtomicPatchRefusal(p, realUtilGo); got != "" {
		t.Errorf("rewriting a file with real content must be allowed:\n%s", got)
	}
	if got := writeAtomicPatchRefusal(p, ""); got != "" {
		t.Errorf("creating a new file must be allowed:\n%s", got)
	}
}

// Deleting the contents of a file that was already empty loses nothing, and
// the destructive guard has always said so. Keep that.
func TestWriteAtomicRefusal_EmptyOverEmptyIsNotDestruction(t *testing.T) {
	p := patches.Patch{Type: patches.TypeFileWriteAtomic, Path: "new.go", Content: ""}

	if got := writeAtomicPatchRefusal(p, ""); got != "" {
		t.Errorf("there was nothing to lose:\n%s", got)
	}
}

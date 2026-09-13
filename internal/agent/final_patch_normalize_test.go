package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/patch/patches"
)

// The two patches that emptied util.go and main.go, verbatim off the proxy.
func theTwoFilesPatches() []patches.Patch {
	return []patches.Patch{
		{
			Type:    patches.TypeFileWriteAtomic,
			Path:    "util.go",
			Search:  "",
			Replace: "package main\n\nfunc Double(n int) int {\n\treturn n * 2\n}\n",
		},
		{
			Type:    patches.TypeFileWriteAtomic,
			Path:    "main.go",
			Search:  "package main\n\nfunc main() {\n}\n",
			Replace: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(Double(21))\n}\n",
		},
	}
}

// An empty search means the replace IS the file. Keeping the declared type and
// moving the content into the field that type reads is the whole fix.
func TestNormalizeFinalPatch_ReplaceWithNoSearchIsTheWholeFile(t *testing.T) {
	got, why := normalizeFinalPatch(theTwoFilesPatches()[0])

	if got.Type != patches.TypeFileWriteAtomic {
		t.Errorf("with nothing to anchor to this is a whole-file write, got %s", got.Type)
	}
	if !strings.Contains(got.Content, "func Double") {
		t.Errorf("the content must survive the relabelling:\n%q", got.Content)
	}
	if got.Replace != "" {
		t.Errorf("replace must be cleared, or the patch still states its intent twice: %q", got.Replace)
	}
	if why == "" {
		t.Error("a reinterpreted patch must say so, or a trace cannot show what was applied")
	}
}

// A filled-in search means the model meant a partial edit, whatever it called
// it. This is the one that matters: every guard downstream switches on Type,
// so the wrong label walks a partial edit past the checks written for it.
func TestNormalizeFinalPatch_SearchAndReplaceIsAPartialEdit(t *testing.T) {
	got, why := normalizeFinalPatch(theTwoFilesPatches()[1])

	if got.Type != patches.TypeFileSearchReplace {
		t.Errorf("a patch carrying search and replace is a partial edit, got %s", got.Type)
	}
	if got.Search == "" || got.Replace == "" {
		t.Error("the edit must survive the relabelling")
	}
	if why == "" {
		t.Error("a reinterpreted patch must say so")
	}
}

// The mirror image, and the reason the rule is stated in terms of fields
// rather than as a special case for one wrong label.
func TestNormalizeFinalPatch_ContentUnderSearchReplaceIsAWholeFile(t *testing.T) {
	got, why := normalizeFinalPatch(patches.Patch{
		Type:    patches.TypeFileSearchReplace,
		Path:    "util.go",
		Content: "package main\n",
	})

	if got.Type != patches.TypeFileWriteAtomic {
		t.Errorf("a patch carrying only content is a whole-file write, got %s", got.Type)
	}
	if why == "" {
		t.Error("a reinterpreted patch must say so")
	}
}

// A well-formed patch must come through untouched. A normaliser that rewrites
// correct input is worse than none: it makes every trace a guess about what
// the model actually sent.
func TestNormalizeFinalPatch_WellFormedPatchesAreLeftAlone(t *testing.T) {
	for _, p := range []patches.Patch{
		{Type: patches.TypeFileSearchReplace, Path: "a.go", Search: "x", Replace: "y"},
		{Type: patches.TypeFileWriteAtomic, Path: "b.go", Content: "package main\n"},
		{Type: patches.TypeFileUnifiedDiff, Path: "c.go", Diff: "@@ -1 +1 @@\n-x\n+y\n"},
	} {
		got, why := normalizeFinalPatch(p)
		if why != "" {
			t.Errorf("%s was reinterpreted and should not have been: %s", p.Type, why)
		}
		if got != p {
			t.Errorf("%s was modified:\ngot  %+v\nwant %+v", p.Type, got, p)
		}
	}
}

// The relabelled partial edit has to reach the guard that exists for partial
// edits. Before this, main.go's patch was labelled write_atomic, and
// restatedPatchHint skips write_atomic on the reasoning that a whole-file
// write cannot double-apply — true of the label, false of the patch.
func TestNormalizeFinalPatch_TheRelabelledEditReachesTheRestatementGuard(t *testing.T) {
	fixed, _ := normalizeFinalPatches(theTwoFilesPatches())

	a := &Agent{}
	a.recordMutatedPath("util.go")
	a.recordMutatedPath("main.go")

	hint := a.restatedPatchHint(fixed)
	if hint == "" {
		t.Fatal("a partial edit restating a change the turn already made must be refused; " +
			"mislabelled as write_atomic it walked straight past this guard")
	}
	if !strings.Contains(hint, "main.go") {
		t.Errorf("the refusal must name the file it is about:\n%s", hint)
	}
}

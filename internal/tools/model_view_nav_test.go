package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture reads a workspace file straight from disk — the check that a
// read-only tool really was read-only.
func readFixture(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return string(b)
}

// The navigation half of the model's view: symbols, repo_map, explore and
// diff.preview. All four are advertised in nearly every mode, all four were
// reached by the eval, and none of them had a test that read the answer.
//
// The eval proves a model CAN get through a task with them. It cannot prove
// the answer was worth reading — a model that gets a useless answer simply
// falls back to read+grep and still passes. That is exactly how `symbols`
// went 17 tasks without a single call.

// ---- symbols ---------------------------------------------------------------

// An outline that omits a symbol is worse than no outline: the model treats
// the list as complete and concludes the symbol is not there.
func TestModelView_SymbolsListsEveryDeclarationInTheFile(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "symbols", map[string]any{"path": "item.go"})
	for _, want := range []string{"Item", "Name", "Qty"} {
		if !strings.Contains(out, `"`+want+`"`) {
			t.Errorf("symbols omitted %q, so the model will believe it does not exist:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "struct") {
		t.Errorf("symbols must say what kind each symbol is; a bare name does not "+
			"tell a model whether it can be called, embedded or assigned:\n%s", out)
	}
}

// symbols handed the model ops.Range — the INTERNAL ops coordinate type, 0-based
// by contract — while every other model-facing view of a line is 1-based:
// `read` prefixes each line "4: ", `explore` says "lines 4-7", `grep` reports
// 1-based lines, and the LSP tools take and return 1-based positions (see
// TestModelView_LspPositionsAreOneBasedLikeRead). For `type Item struct` on the
// fourth line of item.go, symbols said line 3 and read said line 4, and nothing
// told the model which it was holding.
//
// ToolsVersion 15: symbols answers start_line/start_col/end_line/end_col, 1-based,
// in the shape the LSP tools already use.
func TestModelView_SymbolsLineNumbersMatchRead(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	// item.go, 1-based:  1 package main | 2 blank | 3 comment | 4 type Item struct
	read := mustCall(t, r, "read", map[string]any{"path": "item.go"})
	if !strings.Contains(read, "4: type Item struct") {
		t.Fatalf("the fixture moved; this test compares the two numberings and needs "+
			"to know where the declaration is:\n%s", read)
	}

	out := mustCall(t, r, "symbols", map[string]any{"path": "item.go"})
	if !strings.Contains(out, `"start_line":4`) {
		t.Errorf("symbols does not put Item on line 4, where read has it:\n%s", out)
	}
	if strings.Contains(out, `"range"`) {
		t.Errorf("symbols still carries the 0-based ops range beside the 1-based lines:\n%s", out)
	}
}

// ---- repo_map --------------------------------------------------------------

// repo_map is what a model reads instead of listing the tree by hand, and the
// explore-first gate accepts it as "you have looked around". An answer that
// names files without saying what is in them does not earn that.
func TestModelView_RepoMapNamesTheFilesAndWhatIsDeclaredInThem(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "repo_map", map[string]any{})
	for _, want := range []string{"mathx.go", "item.go", "internal/store/s.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("repo_map did not mention %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Add") || !strings.Contains(out, "Item") {
		t.Errorf("repo_map listed files but not their symbols; that is `ls` with extra "+
			"steps, and a model still has to open every file:\n%s", out)
	}
}

// The two Items live in different packages. A map that prints the name twice
// with no way to tell them apart sends the model to edit the wrong one.
func TestModelView_RepoMapKeepsTwoSameNamedTypesApart(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "repo_map", map[string]any{})
	// The answer is JSON, so the newlines inside `text` arrive escaped: the
	// file heading reads `\nitem.go`, two characters, not one.
	if !strings.Contains(out, "internal/store/s.go") || !strings.Contains(out, `\nitem.go`) {
		t.Fatalf("both files must appear, each as its own heading:\n%s", out)
	}
	// Each Item has to sit under its own file heading, not in one flat list.
	if strings.Count(out, "Item") < 2 {
		t.Errorf("only one of the two Item declarations is visible:\n%s", out)
	}
}

// ---- explore ---------------------------------------------------------------

// An ambiguous name is the normal case in a real repo, and the useful answer
// is not "2 matches" — it is which two, where, and what to ask for instead.
func TestModelView_ExploreOnAnAmbiguousNameSaysWhichCandidatesAndWhere(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "explore", map[string]any{"symbol_name": "Item"})
	for _, want := range []string{"internal/store", "item.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("explore reported ambiguity without naming %q, so the model cannot "+
				"disambiguate and has to fall back to grep:\n%s", want, out)
		}
	}
	// The qualified names are what the model must send back; without them the
	// answer says "be more specific" and hides the way to be more specific.
	if !strings.Contains(out, "evalws.Item") {
		t.Errorf("explore must hand back the qualified name to retry with:\n%s", out)
	}
}

// An unqualified miss must not look like an empty repository.
func TestModelView_ExploreOnAnUnknownSymbolSaysSoRatherThanAnsweringEmpty(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, _ := call(t, r, "explore", map[string]any{"symbol_name": "NoSuchSymbolAnywhere"})
	if strings.TrimSpace(out) == "" || out == `{"content":""}` {
		t.Errorf("an empty answer reads as 'the tool is broken' and costs the model a "+
			"retry; it has to say the symbol was not found:\n%s", out)
	}
}

// Tool answers to the model are English. explore was the exception: every
// heading, hint and "not found" came back in Russian.
func TestModelView_ExploreAnswersInEnglish(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	for _, name := range []string{"Item", "evalws.Item", "NoSuchSymbolAnywhere", "Ite", "internal/store"} {
		out, _ := call(t, r, "explore", map[string]any{"symbol_name": name})
		for _, ch := range out {
			if ch >= 0x0400 && ch <= 0x04FF {
				t.Errorf("explore(%q) answers in Russian:\n%s", name, out)
				break
			}
		}
	}
}

// ---- diff.preview ----------------------------------------------------------

// diff.preview exists so a model can look before it leaps. Its whole value is
// that the preview shows the change AND leaves the file alone — a preview that
// writes is an edit with a misleading name.
func TestModelView_DiffPreviewShowsBothSidesOfTheChange(t *testing.T) {
	r, root := modelViewWorkspace(t)

	before := readFixture(t, root, "mathx.go")
	out := mustCall(t, r, "diff.preview", map[string]any{
		"path":    "mathx.go",
		"search":  "a + b",
		"replace": "a + b + 0",
	})
	if !strings.Contains(out, "-\\treturn a + b") && !strings.Contains(out, "-\treturn a + b") {
		t.Errorf("the preview does not show the line being removed:\n%s", out)
	}
	if !strings.Contains(out, "+\\treturn a + b + 0") && !strings.Contains(out, "+\treturn a + b + 0") {
		t.Errorf("the preview does not show the line being added:\n%s", out)
	}
	if after := readFixture(t, root, "mathx.go"); after != before {
		t.Errorf("diff.preview wrote to the file; it is a preview:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A preview whose search text is not in the file must fail the way `edit`
// fails — that is the entire point of previewing first.
func TestModelView_DiffPreviewOnTextThatIsNotThereSaysSo(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "diff.preview", map[string]any{
		"path":    "mathx.go",
		"search":  "return a * b",
		"replace": "return a + b",
	})
	if err == nil && !strings.Contains(strings.ToLower(out), "not found") &&
		!strings.Contains(strings.ToLower(out), "no match") {
		t.Errorf("a preview of a change that cannot be applied came back looking fine; "+
			"the model will now call edit and be surprised:\n%s", out)
	}
}

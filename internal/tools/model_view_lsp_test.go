package tools_test

import (
	"strings"
	"testing"
)

// lsp.hover and lsp.rename are advertised in nearly every mode and neither was
// ever called by a run. lsp_explains_a_symbol offers the model either hover or
// definition and it always picks definition, so hover has no eval coverage and
// is unlikely to get any — which makes the contract level the only level it
// can have.

// hover is the one tool that answers "what IS this" in one call: the signature
// a caller needs and the doc comment saying what it means. An answer with the
// signature and no doc, or a doc and no signature, sends the model to read the
// file anyway and the call was wasted.
func TestModelView_HoverGivesTheSignatureAndTheDocComment(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	// mathx.go, 1-based: 1 package | 2 blank | 3 doc comment | 4 func Add(...)
	out := mustCall(t, r, "lsp.hover", map[string]any{"path": "mathx.go", "line": 4, "col": 6})
	if !strings.Contains(out, "func Add") {
		t.Errorf("hover did not give the signature:\n%s", out)
	}
	if !strings.Contains(out, "sum of a and b") {
		t.Errorf("hover gave the signature without the doc comment, which is the half that "+
			"says what the function means:\n%s", out)
	}
}

// Hovering over whitespace is what happens when the model is one line off, and
// it must read as "nothing here" rather than as a broken tool — otherwise the
// model retries the same call instead of correcting the position.
func TestModelView_HoverOnNothingSaysSoInsteadOfFailing(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "lsp.hover", map[string]any{"path": "mathx.go", "line": 2, "col": 1})
	if err != nil {
		t.Errorf("hovering over a blank line came back as an error:\n%s", out)
	}
	if strings.TrimSpace(out) == "" || out == `{"content":""}` {
		t.Errorf("an empty answer gives the model nothing to distinguish 'no symbol here' "+
			"from 'the tool is broken':\n%s", out)
	}
}

// lsp.rename PROPOSES. Its description says so — "Returns the proposed edits,
// which you then apply with edit or write" — and this pins the behaviour that
// description promises, in both halves.
//
// The half that matters is the second. fs.delete and fs.rename answered as if
// they had done the work while doing nothing, and the model deleted the same
// file until the turn died. lsp.rename is the same shape done right: it
// changes nothing AND is documented as changing nothing. If it ever starts
// writing, the description becomes the lie instead.
func TestModelView_LspRenameProposesEditsAndTouchesNothing(t *testing.T) {
	r, root := modelViewWorkspace(t)

	before := readFixture(t, root, "mathx.go")
	beforeCaller := readFixture(t, root, "main.go")

	out := mustCall(t, r, "lsp.rename", map[string]any{
		"path": "mathx.go", "line": 4, "col": 6, "new_name": "Sum",
	})

	// The declaration and the call site both have to be in the proposal, or
	// applying it breaks the build the model was trying to keep working.
	if !strings.Contains(out, "mathx.go") {
		t.Errorf("the proposal omits the declaration's own file:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("the proposal omits main.go, where Add is called; applying it would leave "+
			"a call to a name that no longer exists:\n%s", out)
	}
	if !strings.Contains(out, "Sum") {
		t.Errorf("the proposal does not carry the new name:\n%s", out)
	}

	if readFixture(t, root, "mathx.go") != before || readFixture(t, root, "main.go") != beforeCaller {
		t.Error("lsp.rename wrote to disk. Its description tells the model to apply the " +
			"edits itself, so the model will now apply them a second time")
	}
}

// Every LSP tool takes 1-based line and col — the schemas say so outright
// ("minimum": 1) — and answers in the same numbering. This is the check that
// keeps TestModelView_SymbolsLineNumbersMatchRead honest: it establishes what
// the other tools actually do rather than assuming it.
func TestModelView_LspPositionsAreOneBasedLikeRead(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	// 1-based line 4 is `func Add(a, b int) int`; 0-based it would be line 3.
	atOne := mustCall(t, r, "lsp.hover", map[string]any{"path": "mathx.go", "line": 4, "col": 6})
	if !strings.Contains(atOne, "func Add") {
		t.Fatalf("hover at 1-based line 4 did not find Add, so this file's assumption about "+
			"the fixture is wrong:\n%s", atOne)
	}
	atZero := mustCall(t, r, "lsp.hover", map[string]any{"path": "mathx.go", "line": 3, "col": 5})
	if strings.Contains(atZero, "func Add") {
		t.Error("hover answers the same symbol for both numberings, so this test can no " +
			"longer tell them apart and the symbols test's premise needs rechecking")
	}
}

// ---- runtime_query ---------------------------------------------------------

// runtime_query reads traces the workspace may simply not have. A model asking
// about a trace id it guessed needs to be told that plainly, with the id echoed
// back, or it cannot tell a wrong id from an empty store.
func TestModelView_RuntimeQueryOnAMissingTraceNamesIt(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "runtime_query", map[string]any{"trace_id": "no-such-trace"})
	if err == nil {
		t.Logf("a missing trace is not an error here; answer: %s", out)
	}
	if !strings.Contains(out, "no-such-trace") {
		t.Errorf("the answer does not echo the trace id that was asked for, so a model "+
			"holding several cannot tell which one came back empty:\n%s", out)
	}
}

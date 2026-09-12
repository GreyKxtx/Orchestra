package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol"
)

// What the model actually receives.
//
// There are 258 tests under internal/tools and they check the tools from the
// inside: this function returns that struct, this resolver refuses that patch.
// None of them reads the text a model is handed, and that text is the whole
// interface — a model cannot call a Go function, it can only act on the bytes
// that come back through Runner.Call.
//
// That is where the expensive defects were. A 91-byte item.go answered with
// "the file may be thousands of lines" and no content. A tool reporting
// bytes_written 0 and success over a file it never created. Both passed every
// test in the package, because from the inside both were doing what they were
// written to do.
//
// So these drive the tools the way the agent loop does — by name, with JSON —
// and assert two things about the answer: that it is TRUE, and that it is
// ENOUGH TO ACT ON. A tool that answers accurately but leaves out the one
// fact the next step needs has failed at its job.

// The eval fixture, near enough: a module whose root package is `main` and
// whose name therefore differs from its import path — the shape that broke.
func modelViewWorkspace(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":              "module evalws\n\ngo 1.21\n",
		"main.go":             "package main\n\nfunc main() {\n\tprintln(Add(1, 2))\n}\n",
		"mathx.go":            "package main\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"item.go":             "package main\n\n// Item is one line of an order.\ntype Item struct {\n\tName string\n\tQty  int\n}\n",
		"internal/store/s.go": "package store\n\n// Item is a stored line.\ntype Item struct{ ID string }\n",
		"README.md":           "# evalws\n\nA tiny Go program used as a fixture.\n",
	}
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

// call runs a tool the way the agent loop does and returns what the model
// would see: the answer as text, or the error as text.
func call(t *testing.T, r *tools.Runner, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args for %s: %v", name, err)
	}
	out, err := r.Call(context.Background(), name, raw)
	if err != nil {
		return err.Error(), err
	}
	return string(out), nil
}

func mustCall(t *testing.T, r *tools.Runner, name string, args map[string]any) string {
	t.Helper()
	out, err := call(t, r, name, args)
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	return out
}

// ---- read ------------------------------------------------------------------

// The defect this whole file exists for. A short Go file must come back as
// itself; the package clause is the one line a whole-file write cannot be
// written without, and no other tool reports it.
func TestModelView_ReadShowsAShortGoFileIncludingItsPackageClause(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "read", map[string]any{"path": "item.go"})
	if !strings.Contains(out, "package main") {
		t.Errorf("read must show the package clause; the model has nowhere else to get it:\n%s", out)
	}
	if !strings.Contains(out, "type Item struct") {
		t.Errorf("read must show the file's code:\n%s", out)
	}
	if strings.Contains(out, "thousands of lines") {
		t.Errorf("a 7-line file was withheld as if it were huge:\n%s", out)
	}
}

// A read of something that is not there has to say where the model actually
// is — a bare OS error hands it nothing but a path to guess at again, which
// is how a turn burns its tool budget on five more guesses.
func TestModelView_ReadingAMissingPathSaysWhereTheWorkspaceIs(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "read", map[string]any{"path": "evalws/item.go"})
	if err == nil {
		t.Fatal("reading a path that does not exist must fail")
	}
	if strings.Contains(out, "GetFileAttributesEx") || strings.Contains(out, "no such file or directory") {
		t.Errorf("the raw OS error tells the model nothing it can use:\n%s", out)
	}
	if !strings.Contains(out, "item.go") && !strings.Contains(out, "workspace") {
		t.Errorf("the error must name what the workspace does hold:\n%s", out)
	}
}

// ---- ls --------------------------------------------------------------------

func TestModelView_LsNamesTheWorkspacesOwnEntries(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "ls", map[string]any{"path": "."})
	for _, want := range []string{"item.go", "mathx.go", "go.mod", "internal"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls must list %q:\n%s", want, out)
		}
	}
}

// Pointing ls at a file is an ordinary slip. The answer has to name the tool
// that would have worked, or the model retries the same call.
func TestModelView_LsOnAFileSaysToUseReadInstead(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out, err := call(t, r, "ls", map[string]any{"path": "item.go"})
	if err == nil {
		t.Fatal("ls on a file must fail rather than pretend")
	}
	if !strings.Contains(out, "read") {
		t.Errorf("the error must point at the tool that works:\n%s", out)
	}
}

// ---- glob ------------------------------------------------------------------

func TestModelView_GlobFindsByPatternAndReportsRelativePaths(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "glob", map[string]any{"pattern": "**/*.go"})
	for _, want := range []string{"item.go", "mathx.go", "internal/store/s.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("glob must find %q:\n%s", want, out)
		}
	}
	// Absolute paths are not something the other tools accept back.
	if strings.Contains(out, ":\\\\") || strings.Contains(out, `:\\`) {
		t.Errorf("glob must answer in workspace-relative paths:\n%s", out)
	}
}

// A pattern that matches nothing must say so plainly. An empty result that
// reads like an error sends the model looking for a problem that is not there.
func TestModelView_GlobWithNoMatchesIsNotAnError(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	if _, err := call(t, r, "glob", map[string]any{"pattern": "**/*.rs"}); err != nil {
		t.Errorf("finding nothing is an answer, not a failure: %v", err)
	}
}

// ---- grep ------------------------------------------------------------------

func TestModelView_GrepReportsTheFileAndLineOfEveryMatch(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "grep", map[string]any{"query": "func Add"})
	if !strings.Contains(out, "mathx.go") {
		t.Errorf("grep must name the file it matched in:\n%s", out)
	}
	if !strings.Contains(out, "Add") {
		t.Errorf("grep must show the matching text:\n%s", out)
	}
}

// The model narrows to one file constantly. If the filter is ignored the
// model reasons about matches from files it deliberately excluded.
func TestModelView_GrepHonoursThePathsFilter(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	out := mustCall(t, r, "grep", map[string]any{"query": "Item", "paths": []string{"item.go"}})
	if !strings.Contains(out, "item.go") {
		t.Errorf("grep must report the match in the file it was pointed at:\n%s", out)
	}
	if strings.Contains(out, "internal/store") {
		t.Errorf("grep was restricted to item.go and answered about another file:\n%s", out)
	}
}

// ---- edit ------------------------------------------------------------------

func TestModelView_EditChangesOnlyWhatItWasAskedTo(t *testing.T) {
	r, root := modelViewWorkspace(t)

	if _, err := call(t, r, "edit", map[string]any{
		"path": "mathx.go", "search": "return a + b", "replace": "return a - b",
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "mathx.go"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "package main\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	if string(got) != want {
		t.Errorf("edit changed more than the one line:\n got %q\nwant %q", got, want)
	}
}

// The failure that cost three eval rounds: the model searches for text it has
// already replaced, five times in a row, until the breaker stops the turn.
//
// What saves it is the excerpt of the file as it actually reads now, carried
// in the error's structured data — the agent loop pastes that into history
// (formatToolErrorJSON), and its own test builds the error by hand. So the
// half nobody checked is this one: that the edit tool really produces it.
// "search block not found" on its own is a dead end, and that message on its
// own is exactly what the loop repeated against.
func TestModelView_EditThatCannotFindItsTextShowsWhatTheFileSaysNow(t *testing.T) {
	r, _ := modelViewWorkspace(t)

	_, err := call(t, r, "edit", map[string]any{
		"path": "mathx.go", "search": "return a * b", "replace": "return a - b",
	})
	if err == nil {
		t.Fatal("an edit whose search text is absent must fail, not silently do nothing")
	}
	pe, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("the refusal must be a protocol error the loop can unpack, got %T: %v", err, err)
	}
	data, _ := pe.Data.(map[string]any)
	if data == nil {
		t.Fatalf("%q carries no detail at all — the model has nothing to work from", pe.Message)
	}
	nearest, _ := data["nearest"].(string)
	if nearest == "" {
		t.Fatalf("the refusal must carry the file as it reads now; data was %#v", data)
	}
	if !strings.Contains(nearest, "a + b") {
		t.Errorf("the excerpt must show the text that is really there:\n%s", nearest)
	}
}

// ---- write -----------------------------------------------------------------

// An overwrite without the hash is how a model clobbers a file it never read.
func TestModelView_WriteOverAnExistingFileNeedsTheHash(t *testing.T) {
	r, root := modelViewWorkspace(t)

	out, err := call(t, r, "write", map[string]any{
		"path": "mathx.go", "content": "package main\n",
	})
	if err == nil {
		t.Fatal("overwriting a file without its hash must be refused")
	}
	if !strings.Contains(out, "file_hash") {
		t.Errorf("the refusal must name what is missing:\n%s", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "mathx.go"))
	if !strings.Contains(string(got), "Add returns the sum") {
		t.Error("the file was changed despite the refusal")
	}
}

// And the answer to a write has to be checkable: a model that is told how
// many bytes landed can tell an empty write from a real one.
func TestModelView_WriteReportsWhatItWrote(t *testing.T) {
	r, root := modelViewWorkspace(t)

	const body = "package main\n\nfunc Sub(a, b int) int {\n\treturn a - b\n}\n"
	out := mustCall(t, r, "write", map[string]any{
		"path": "subx.go", "content": body, "must_not_exist": true,
	})
	if !strings.Contains(out, "subx.go") {
		t.Errorf("the answer must name the file written:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(root, "subx.go"))
	if err != nil {
		t.Fatalf("write reported success and created nothing: %v", err)
	}
	if string(got) != body {
		t.Errorf("the file on disk is not what was asked for:\n%q", got)
	}
}

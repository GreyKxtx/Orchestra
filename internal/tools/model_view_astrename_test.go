package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// ast_rename is a mutating tool with an eval pass and no response contract. Its
// description makes three promises the model acts on and nothing checks:
// it skips string literals, comments and substring hits; it works within ONE
// file; and it goes through the same staging and file_hash path as write.
//
// The third is the one that bit before: fs.delete and fs.rename answered
// "deleted"/"renamed" under the agent while doing nothing, because their
// dry-run branch reported success and they had no commit path. ast_rename
// writes through c.Write, so it should stage like write — the test asks the
// overlay, not the disk.

const astRenameFixture = `package main

// Width is the frame width. Do not confuse with WidthPx.
func Width() int {
	return 640
}

func describe() string {
	return "Width in pixels"
}

func WidthPx() int {
	return Width() * 2
}
`

func astRenameWorkspace(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":   "module evalws\n\ngo 1.21\n",
		"width.go": astRenameFixture,
		"other.go": "package main\n\nfunc other() int {\n\treturn Width()\n}\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, root
}

// renameOrSkip runs the tool and skips the test when this build cannot parse
// the language, so a missing parser reads as "not measured" rather than as a
// passing test.
func renameOrSkip(t *testing.T, r *tools.Runner, args map[string]any) string {
	t.Helper()
	out, err := call(t, r, "ast_rename", args)
	if err != nil && strings.Contains(strings.ToLower(out), "unsupported") {
		t.Skipf("this build cannot parse the fixture language: %s", out)
	}
	if err != nil {
		t.Fatalf("ast_rename failed: %s", out)
	}
	return out
}

// The promise the tool exists for. edit on "Width" would hit the comment, the
// string and WidthPx; if ast_rename does the same, the model is better off
// with edit and the description is a lie.
func TestModelView_AstRenameSkipsStringsCommentsAndSubstrings(t *testing.T) {
	r, root := astRenameWorkspace(t)

	out := renameOrSkip(t, r, map[string]any{
		"path": "width.go", "old_name": "Width", "new_name": "Frame",
	})
	after := readFixture(t, root, "width.go")

	if strings.Contains(after, "func Width()") {
		t.Errorf("the identifier was not renamed:\n%s", after)
	}
	if !strings.Contains(after, "func Frame()") {
		t.Errorf("the new name is not in the file:\n%s", after)
	}
	if !strings.Contains(after, "// Width is the frame width") {
		t.Errorf("the comment was rewritten; the tool promises to skip comments:\n%s", after)
	}
	if !strings.Contains(after, `"Width in pixels"`) {
		t.Errorf("the string literal was rewritten; the tool promises to skip strings:\n%s", after)
	}
	if !strings.Contains(after, "func WidthPx()") {
		t.Errorf("a substring hit was rewritten; the tool promises to skip substrings:\n%s", after)
	}
	if !strings.Contains(out, `"count"`) {
		t.Errorf("the answer carries no count, so the model cannot tell one rename from none:\n%s", out)
	}
}

// "within one file" is in the description, and the model has to believe it:
// after renaming a function used elsewhere, the other caller still says the old
// name and the package no longer compiles. The tool must not quietly suggest
// otherwise by reporting a count that covers files it never touched.
func TestModelView_AstRenameTouchesOnlyTheFileItWasGiven(t *testing.T) {
	r, root := astRenameWorkspace(t)
	before := readFixture(t, root, "other.go")

	renameOrSkip(t, r, map[string]any{
		"path": "width.go", "old_name": "Width", "new_name": "Frame",
	})

	if after := readFixture(t, root, "other.go"); after != before {
		t.Errorf("ast_rename changed a second file; its description promises one:\n%s", after)
	}
}

// A name that is not there is not an error: the model asked a reasonable
// question and must get "nothing matched" so it can look elsewhere, not a
// failure it will retry. And nothing may be written for a zero-count rename.
func TestModelView_AstRenameOnAMissingNameSaysNothingMatchedAndWritesNothing(t *testing.T) {
	r, root := astRenameWorkspace(t)
	before := readFixture(t, root, "width.go")

	out, err := call(t, r, "ast_rename", map[string]any{
		"path": "width.go", "old_name": "NoSuchSymbol", "new_name": "Frame",
	})
	if err != nil {
		t.Fatalf("a name that does not occur came back as a tool failure:\n%s", out)
	}
	if !strings.Contains(out, `"count":0`) {
		t.Errorf("the answer does not report zero matches, which is the one fact the model "+
			"needs to stop retrying:\n%s", out)
	}
	if strings.Contains(out, `"wrote":true`) {
		t.Errorf("a rename that matched nothing reported a write:\n%s", out)
	}
	if after := readFixture(t, root, "width.go"); after != before {
		t.Errorf("the file changed for a rename that matched nothing:\n%s", after)
	}
}

// "Goes through the same staging and file_hash path as write" — so in a dry-run
// the disk must be untouched and the change must be in the overlay, visible to
// read. fs.delete and fs.rename claimed success in exactly this situation while
// the overlay stayed empty and the model deleted the same file until the turn
// died.
func TestModelView_AstRenameStagesLikeWriteInsteadOfTouchingDisk(t *testing.T) {
	r, root := astRenameWorkspace(t)
	r.SetDryRun(true)
	before := readFixture(t, root, "width.go")

	renameOrSkip(t, r, map[string]any{
		"path": "width.go", "old_name": "Width", "new_name": "Frame",
	})

	if after := readFixture(t, root, "width.go"); after != before {
		t.Errorf("a dry-run rename wrote to disk:\n%s", after)
	}
	if len(r.StagedOps(context.Background())) == 0 {
		t.Fatal("a dry-run rename reported success and staged nothing: the work exists nowhere")
	}
	// The model's next read must see its own rename, or it re-does it.
	readBack := mustCall(t, r, "read", map[string]any{"path": "width.go"})
	if !strings.Contains(readBack, "func Frame()") {
		t.Errorf("read does not show the staged rename, so the model cannot see its own work:\n%s", readBack)
	}
}

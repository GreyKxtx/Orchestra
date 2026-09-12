package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// The tree-sitter gate rejected a broken file before staging it — on the
// dry-run path only. The apply path, where the bytes actually reach the disk,
// went straight to the applier and never asked.
//
// So the check ran exactly where nothing could be damaged and stayed silent
// where something could.
//
// What the gate can see is bounded by the tree-sitter grammar, which is
// deliberately forgiving: a .go file with no package clause parses fine here
// even though the Go compiler rejects it, so that particular breakage is not
// covered. An unclosed brace is, and it is the ordinary way a generated edit
// leaves a file broken.
const brokenGo = "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n"

func applyRunner(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestWrite_RefusesBrokenSyntaxOnTheApplyPath(t *testing.T) {
	r, root := applyRunner(t)
	const good = "package main\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(filepath.Join(root, "mathx.go"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "mathx.go"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}

	_, err = r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "mathx.go", Content: brokenGo, FileHash: before.FileHash,
	})
	if err == nil {
		t.Fatal("writing a Go file with an unclosed brace must be refused")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "syntax") {
		t.Errorf("the refusal must say what is wrong, got: %v", err)
	}

	on, readErr := os.ReadFile(filepath.Join(root, "mathx.go"))
	if readErr != nil {
		t.Fatalf("the file must still be there: %v", readErr)
	}
	if string(on) != good {
		t.Errorf("a refused write must not touch the file on disk, got:\n%s", on)
	}
}

func TestEdit_RefusesBrokenSyntaxOnTheApplyPath(t *testing.T) {
	r, root := applyRunner(t)
	const good = "package main\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(filepath.Join(root, "mathx.go"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	// Delete the closing brace: valid search, broken result.
	_, err := r.FSEdit(context.Background(), tools.FSEditRequest{
		Path:    "mathx.go",
		Search:  "\treturn a - b\n}\n",
		Replace: "\treturn a - b\n",
	})
	if err == nil {
		t.Fatal("an edit that leaves the file unparseable must be refused")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "syntax") {
		t.Errorf("the refusal must say what is wrong, got: %v", err)
	}
	on, _ := os.ReadFile(filepath.Join(root, "mathx.go"))
	if string(on) != good {
		t.Errorf("a refused edit must not touch the file on disk, got:\n%s", on)
	}
}

// The gate only knows the languages the CKG has a grammar for; everything
// else must pass through untouched.
func TestWrite_LeavesFilesWithNoGrammarAlone(t *testing.T) {
	r, root := applyRunner(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "notes.txt", Content: "}}} not code at all {{{\n", FileHash: before.FileHash,
	}); err != nil {
		t.Errorf("a text file must not be judged by a code grammar: %v", err)
	}
}

// And valid code still writes.
func TestWrite_AllowsValidCodeOnTheApplyPath(t *testing.T) {
	r, root := applyRunner(t)
	if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "greet.go", Content: "package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n", MustNotExist: true,
	}); err != nil {
		t.Fatalf("valid Go must be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "greet.go")); err != nil {
		t.Errorf("the file should exist: %v", err)
	}
}

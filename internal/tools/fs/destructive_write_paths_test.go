package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/patch/resolver"
)

// The destructive-write guard already had unit tests, and every one of them
// called resolveWriteAtomic — the package-private function — directly. That
// proves the rule and nothing about whether the rule is reachable.
//
// A model does not call resolveWriteAtomic. It has three ways to replace a
// whole file, and each goes through different code:
//
//   - the write tool while the run applies, which resolves then applies;
//   - the write tool while the run is a dry run, which stages and never
//     reaches patch resolution at all;
//   - a file.write_atomic in final.patches, which the agent loop hands to
//     ResolveExternalPatches.
//
// This file drives all three, and asserts on the file as well as on the
// error: a refusal that still lets the bytes through is not a refusal.

const guardedGo = "package main\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"

func guardedRunner(t *testing.T, dryRun bool) (*tools.Runner, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mathx.go"), []byte(guardedGo), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: dryRun})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root, cache.ComputeSHA256([]byte(guardedGo))
}

func onDisk(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("reading back %s: %v", name, err)
	}
	return string(b)
}

// The shape that destroyed a file in an evaluation run: the model had already
// edited mathx.go correctly, then closed the turn with a whole-file write
// carrying the right hash and no content at all.
func TestWrite_ApplyPathRefusesToEmptyAFile(t *testing.T) {
	r, root, hash := guardedRunner(t, false)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "mathx.go", Content: "", FileHash: hash,
	})
	if err == nil {
		t.Fatal("the write tool emptied an existing file without complaint")
	}
	if !strings.Contains(err.Error(), "mathx.go") {
		t.Errorf("the refusal must name the file, got: %v", err)
	}
	if got := onDisk(t, root, "mathx.go"); got != guardedGo {
		t.Errorf("the file was changed despite the refusal:\n%q", got)
	}
}

// Staging bypasses patch resolution entirely, so the guard has to be asked a
// second time on this path. Without it a dry run stages the file emptied and
// the model reads its own damage back as success.
func TestWrite_DryRunPathRefusesToEmptyAFile(t *testing.T) {
	r, root, hash := guardedRunner(t, true)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "mathx.go", Content: "", FileHash: hash,
	})
	if err == nil {
		t.Fatal("staging accepted a write that empties an existing file")
	}
	if got := onDisk(t, root, "mathx.go"); got != guardedGo {
		t.Errorf("a dry run must not touch the disk at all, got:\n%q", got)
	}
	// And the staged view must still be the real file: a model that reads it
	// back has to see the file it actually has.
	back, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "mathx.go"})
	if err != nil {
		t.Fatalf("FSRead after the refusal: %v", err)
	}
	if !strings.Contains(back.Content, "func Add") {
		t.Errorf("the refused write was staged anyway; reading back gives:\n%q", back.Content)
	}
}

// The agent's own final-patch path. ResolveExternalPatches is what the loop
// calls, and it is the entry the earlier unit tests stepped over.
func TestResolveExternalPatches_RefusesAWholeFileWriteThatEmptiesTheFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mathx.go"), []byte(guardedGo), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := cache.ComputeSHA256([]byte(guardedGo))

	_, err := resolver.ResolveExternalPatches(root, []patches.Patch{{
		Type:       patches.TypeFileWriteAtomic,
		Path:       "mathx.go",
		Content:    "",
		Conditions: &patches.WriteAtomicConditions{FileHash: hash},
	}})
	if err == nil {
		t.Fatal("the agent's final-patch path resolved a write that empties the file")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empt") {
		t.Errorf("the refusal must say what was wrong, got: %v", err)
	}
}

// Creating a genuinely new empty file is ordinary and must stay allowed —
// the guard is about erasing content, not about empty files.
func TestWrite_CreatingAnEmptyFileIsStillAllowed(t *testing.T) {
	r, root, _ := guardedRunner(t, false)

	if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: "empty.txt", Content: "", MustNotExist: true,
	}); err != nil {
		t.Fatalf("creating an empty file must be allowed: %v", err)
	}
	if got := onDisk(t, root, "empty.txt"); got != "" {
		t.Errorf("expected an empty file, got %q", got)
	}
}

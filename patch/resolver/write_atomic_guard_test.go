package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/patches"
)

// Both cases below are transcripts of a real evaluation run against a local
// model. In each, the model had already written the file correctly with the
// write tool, then closed the turn with a final file.write_atomic patch that
// carried the right file_hash and the wrong content. The hash condition was
// satisfied, so the applier wrote it — and the model's own correct work was
// destroyed.
//
// A patch that replaces a file with nothing, or with a note saying where the
// content would have been, is never what anyone meant. It has to be refused
// at resolution, not applied and mourned afterwards.

func guardWorkspace(t *testing.T, name, content string) (root, hash string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cache.ComputeSHA256([]byte(content))
}

func writeAtomicPatch(path, content, fileHash string) patches.Patch {
	return patches.Patch{
		Type:       patches.TypeFileWriteAtomic,
		Path:       path,
		Content:    content,
		Conditions: &patches.WriteAtomicConditions{FileHash: fileHash},
	}
}

func TestWriteAtomic_RefusesToEmptyAnExistingFile(t *testing.T) {
	const body = "package main\n\nimport \"fmt\"\n\nfunc (i Item) Label() string {\n\treturn fmt.Sprint(i.Name)\n}\n"
	root, hash := guardWorkspace(t, "item.go", body)

	_, err := resolveWriteAtomic(root, writeAtomicPatch("item.go", "", hash))
	if err == nil {
		t.Fatal("a write_atomic that empties an existing file must be refused")
	}
	if !strings.Contains(err.Error(), "item.go") {
		t.Errorf("the refusal must name the file, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empt") {
		t.Errorf("the refusal must say what was wrong with the patch, got: %v", err)
	}
}

func TestWriteAtomic_RefusesElidedContent(t *testing.T) {
	// A 300-line file the model summarised away.
	var b strings.Builder
	b.WriteString("package main\n\nconst MaxRetries = 10\n")
	for i := 0; i < 120; i++ {
		b.WriteString("\nfunc step(n int) int {\n\treturn n\n}\n")
	}
	root, hash := guardWorkspace(t, "limits.go", b.String())

	for _, elided := range []string{
		"// ... (file content with MaxRetries = 10) ...",
		"// ... rest of the file unchanged ...",
		"# ... existing code ...",
		"...",
	} {
		_, err := resolveWriteAtomic(root, writeAtomicPatch("limits.go", elided, hash))
		if err == nil {
			t.Errorf("write_atomic with elided content %q must be refused", elided)
			continue
		}
		if !strings.Contains(err.Error(), "limits.go") {
			t.Errorf("the refusal must name the file, got: %v", err)
		}
	}
}

// The third shape, from the run after the first two were guarded: the model
// changed MaxRetries correctly at step 7 (5684 bytes on disk), then at step 8
// rewrote the whole file as the single line it had just changed. 309 lines
// became one. No ellipsis, not empty — just a fragment of the file offered as
// the file.
func TestWriteAtomic_RefusesAFragmentOfTheFileAsTheWholeFile(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nconst (\n\tMaxRetries    = 10\n\tMaxIdleConns  = 64\n)\n")
	for i := 0; i < 120; i++ {
		b.WriteString("\nfunc step(n int) int {\n\treturn n\n}\n")
	}
	root, hash := guardWorkspace(t, "limits.go", b.String())

	_, err := resolveWriteAtomic(root, writeAtomicPatch("limits.go", "\tMaxRetries    = 10\n", hash))
	if err == nil {
		t.Fatal("rewriting a file as one line lifted verbatim out of it must be refused")
	}
	if !strings.Contains(err.Error(), "limits.go") {
		t.Errorf("the refusal must name the file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "edit") {
		t.Errorf("the refusal must point at the tool that changes one part, got: %v", err)
	}
}

// The guard must not get in the way of legitimate work.
func TestWriteAtomic_AllowsRealRewrites(t *testing.T) {
	const body = "package main\n\nfunc Timeout() int {\n\treturn 30\n}\n"
	root, hash := guardWorkspace(t, "config.go", body)

	// A genuine rewrite, shorter than the original but real code.
	if _, err := resolveWriteAtomic(root, writeAtomicPatch("config.go", "package main\n\nfunc Timeout() int { return 60 }\n", hash)); err != nil {
		t.Errorf("a real rewrite must be allowed: %v", err)
	}

	// Prose that happens to contain an ellipsis, in a file that is not code.
	rootMD, hashMD := guardWorkspace(t, "notes.md", "# Notes\n\nold\n")
	long := "# Notes\n\nThe plan is simple... we ship it, then we measure.\nSecond line so this is clearly a document.\n"
	if _, err := resolveWriteAtomic(rootMD, writeAtomicPatch("notes.md", long, hashMD)); err != nil {
		t.Errorf("prose containing an ellipsis must be allowed: %v", err)
	}

	// A big deletion that is NOT a verbatim slice of the original is real
	// work — cutting a file down and rewriting what is left must still pass.
	var big strings.Builder
	big.WriteString("package main\n")
	for i := 0; i < 200; i++ {
		big.WriteString("\nfunc old(n int) int {\n\treturn n\n}\n")
	}
	rootBig, hashBig := guardWorkspace(t, "big.go", big.String())
	if _, err := resolveWriteAtomic(rootBig, writeAtomicPatch("big.go", "package main\n\nfunc kept(n int) int {\n\treturn n * 2\n}\n", hashBig)); err != nil {
		t.Errorf("cutting a file down to newly written code must be allowed: %v", err)
	}
}

// Creating a new empty file is legitimate — the guard is about destroying
// content that exists.
func TestWriteAtomic_AllowsCreatingAnEmptyFile(t *testing.T) {
	root := t.TempDir()
	p := patches.Patch{
		Type:       patches.TypeFileWriteAtomic,
		Path:       "placeholder.txt",
		Content:    "",
		Conditions: &patches.WriteAtomicConditions{MustNotExist: true},
	}
	if _, err := resolveWriteAtomic(root, p); err != nil {
		t.Errorf("creating a new empty file must be allowed: %v", err)
	}
}

// Emptying a file that is already empty changes nothing and must not fail.
func TestWriteAtomic_AllowsEmptyingAnAlreadyEmptyFile(t *testing.T) {
	root, hash := guardWorkspace(t, "empty.txt", "")
	if _, err := resolveWriteAtomic(root, writeAtomicPatch("empty.txt", "", hash)); err != nil {
		t.Errorf("rewriting an empty file as empty must be allowed: %v", err)
	}
}

package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

func TestResolveExternalPatches_SearchReplace_ToOps_Apply(t *testing.T) {
	root := t.TempDir()
	path := "a.txt"
	abs := filepath.Join(root, path)

	before := "hello old world\n"
	if err := os.WriteFile(abs, []byte(before), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := cache.ComputeSHA256([]byte(before))

	ops, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:     patches.TypeFileSearchReplace,
			Path:     path,
			Search:   "old",
			Replace:  "new",
			FileHash: h,
		},
	})
	if err != nil {
		t.Fatalf("ResolveExternalPatches failed: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}

	_, err = applier.ApplyAnyOps(root, ops, applier.ApplyOptions{DryRun: false, Backup: false})
	if err != nil {
		t.Fatalf("ApplyOps failed: %v", err)
	}

	afterBytes, _ := os.ReadFile(abs)
	if string(afterBytes) != "hello new world\n" {
		t.Fatalf("unexpected file content: %q", string(afterBytes))
	}
}

func TestResolveExternalPatches_SearchReplace_Ambiguous(t *testing.T) {
	root := t.TempDir()
	path := "a.txt"
	abs := filepath.Join(root, path)

	before := "dup\ndup\n"
	if err := os.WriteFile(abs, []byte(before), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:     patches.TypeFileSearchReplace,
			Path:     path,
			Search:   "dup",
			Replace:  "x",
			FileHash: cache.ComputeSHA256([]byte(before)),
		},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.AmbiguousMatch {
		t.Fatalf("expected %s, got %s", protocol.AmbiguousMatch, coreErr.Code)
	}
}

func TestResolveExternalPatches_UnifiedDiff_ToOps_Apply(t *testing.T) {
	root := t.TempDir()
	path := "a.txt"
	abs := filepath.Join(root, path)

	before := "a\nb\nc\n"
	if err := os.WriteFile(abs, []byte(before), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	diff := `@@ -2,1 +2,1 @@
-b
+BBB
`

	ops, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:     patches.TypeFileUnifiedDiff,
			Path:     path,
			Diff:     diff,
			FileHash: cache.ComputeSHA256([]byte(before)),
		},
	})
	if err != nil {
		t.Fatalf("ResolveExternalPatches failed: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}

	_, err = applier.ApplyAnyOps(root, ops, applier.ApplyOptions{DryRun: false, Backup: false})
	if err != nil {
		t.Fatalf("ApplyOps failed: %v", err)
	}

	afterBytes, _ := os.ReadFile(abs)
	if string(afterBytes) != "a\nBBB\nc\n" {
		t.Fatalf("unexpected file content: %q", string(afterBytes))
	}
}

func TestResolveExternalPatches_WriteAtomic_ToOps_Apply(t *testing.T) {
	root := t.TempDir()

	ops, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileWriteAtomic,
			Path:    "new.txt",
			Content: "hello\n",
			Mode:    420,
			Conditions: &patches.WriteAtomicConditions{
				MustNotExist: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("ResolveExternalPatches failed: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}

	_, err = applier.ApplyAnyOps(root, ops, applier.ApplyOptions{DryRun: false, Backup: false})
	if err != nil {
		t.Fatalf("ApplyAnyOps failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatalf("read new file failed: %v", err)
	}
	if string(b) != "hello\n" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
}

func TestResolveExternalPatches_WriteAtomic_PathTraversal_Rejected(t *testing.T) {
	root := t.TempDir()

	_, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileWriteAtomic,
			Path:    "../evil.txt",
			Content: "nope",
			Conditions: &patches.WriteAtomicConditions{
				MustNotExist: true,
			},
		},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.PathTraversal {
		t.Fatalf("expected %s, got %s", protocol.PathTraversal, coreErr.Code)
	}
}

// --- forgiving (line-trimmed) path ---

func TestResolveSearchReplace_Forgiving_TrailingWhitespace(t *testing.T) {
	root := t.TempDir()
	path := "a.go"
	abs := filepath.Join(root, path)
	// File has trailing spaces after "1" but before "\n". The LLM-supplied
	// search includes the newline but no trailing spaces, so strict
	// findUnique does *not* see "x = 1\n" as a substring (the actual
	// bytes between '1' and '\n' are three spaces). The forgiving path
	// must recover and pin Expected to the verbatim file bytes.
	original := "package a\n\nx = 1   \nfunc f() {}\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "x = 1\n",
			Replace: "x = 2\n",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].ReplaceRange == nil {
		t.Fatalf("expected 1 replace_range op, got %#v", got)
	}
	op := got[0].ReplaceRange
	if op.Expected != "x = 1   \n" {
		t.Errorf("Expected: want %q (verbatim file bytes), got %q", "x = 1   \n", op.Expected)
	}
	if op.Replacement != "x = 2\n" {
		t.Errorf("Replacement: want %q, got %q", "x = 2\n", op.Replacement)
	}
}

func TestResolveSearchReplace_Forgiving_CRLF(t *testing.T) {
	root := t.TempDir()
	path := "b.go"
	abs := filepath.Join(root, path)
	original := "package b\r\nx := 1\r\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "x := 1\n",
			Replace: "x := 2\n",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	op := got[0].ReplaceRange
	if op.Expected != "x := 1\r\n" {
		t.Errorf("Expected: want CRLF-preserved %q, got %q", "x := 1\r\n", op.Expected)
	}
}

func TestResolveSearchReplace_Forgiving_AmbiguousFails(t *testing.T) {
	root := t.TempDir()
	path := "c.go"
	abs := filepath.Join(root, path)
	original := "y = 1   \ny = 1\t\n" // both lines normalize to "y = 1\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "y = 1",
			Replace: "y = 2",
		},
	})
	if err == nil {
		t.Fatal("expected AmbiguousMatch, got nil")
	}
	pErr, ok := protocol.AsError(err)
	if !ok || pErr.Code != protocol.AmbiguousMatch {
		t.Fatalf("expected AmbiguousMatch protocol error, got %v", err)
	}
}

func TestResolveSearchReplace_Forgiving_StillStaleWhenUnrecoverable(t *testing.T) {
	root := t.TempDir()
	path := "d.go"
	abs := filepath.Join(root, path)
	original := "package d\nz = 1\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "totally absent",
			Replace: "...",
		},
	})
	pErr, ok := protocol.AsError(err)
	if !ok || pErr.Code != protocol.StaleContent {
		t.Fatalf("expected StaleContent, got %v", err)
	}
}

func TestLineTrimmedFind_Unit(t *testing.T) {
	cases := []struct {
		name     string
		hay      string
		needle   string
		wantS    int
		wantE    int
		wantHits int
	}{
		{"trailing spaces in hay", "foo   \nbar\n", "foo\n", 0, 7, 1},
		{"crlf in hay only", "foo\r\nbar\n", "foo\nbar", 0, 8, 1},
		{"empty needle", "anything", "", 0, 0, 0},
		{"absent", "abc\n", "xyz", 0, 0, 0},
		{"two matches", "x\t\nx \n", "x\n", 0, 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e, hits := lineTrimmedFind(c.hay, c.needle)
			if hits != c.wantHits {
				t.Fatalf("hits: want %d, got %d", c.wantHits, hits)
			}
			if hits == 1 {
				if s != c.wantS || e != c.wantE {
					t.Errorf("offsets: want [%d,%d), got [%d,%d). substring=%q", c.wantS, c.wantE, s, e, c.hay[s:e])
				}
			}
		})
	}
}

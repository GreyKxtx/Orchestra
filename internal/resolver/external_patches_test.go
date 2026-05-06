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

func TestNormalizeLeadingAndTrailingWS(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"tab expanded to 4 spaces", "\thello\n", "    hello\n"},
		{"two tabs expanded", "\t\thello\n", "        hello\n"},
		{"spaces kept as-is", "    hello\n", "    hello\n"},
		{"trailing spaces stripped", "foo   \n", "foo\n"},
		{"crlf collapsed", "foo\r\n", "foo\n"},
		{"tab in non-leading position kept", "foo\tbar\n", "foo\tbar\n"},
		{"empty string", "", ""},
		{"no newline at end", "\thello", "    hello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, origIdx := normalizeLeadingAndTrailingWS(c.input)
			if got != c.want {
				t.Errorf("normalized: want %q, got %q", c.want, got)
			}
			if len(origIdx) != len(got)+1 {
				t.Errorf("origIdx length: want %d, got %d", len(got)+1, len(origIdx))
			}
			if len(c.input) > 0 && origIdx[len(origIdx)-1] != len(c.input) {
				t.Errorf("origIdx sentinel: want %d, got %d", len(c.input), origIdx[len(origIdx)-1])
			}
		})
	}
}

func TestIndentFlexibleFind_Unit(t *testing.T) {
	cases := []struct {
		name     string
		hay      string
		needle   string
		wantS    int
		wantE    int
		wantHits int
	}{
		{
			// File uses tab indentation; LLM supplied 4 spaces.
			name:     "tab in file spaces in needle",
			hay:      "\thello\n\tworld\n",
			needle:   "    hello\n    world\n",
			wantS:    0,
			wantE:    14,
			wantHits: 1,
		},
		{
			// File uses 4 spaces; LLM supplied tab.
			name:     "spaces in file tab in needle",
			hay:      "    hello\n    world\n",
			needle:   "\thello\n\tworld\n",
			wantS:    0,
			wantE:    20,
			wantHits: 1,
		},
		{
			name:     "empty needle",
			hay:      "anything\n",
			needle:   "",
			wantHits: 0,
		},
		{
			name:     "absent",
			hay:      "\thello\n",
			needle:   "    goodbye\n",
			wantHits: 0,
		},
		{
			name:     "two identical blocks are ambiguous",
			hay:      "\thello\n\thello\n",
			needle:   "    hello\n",
			wantHits: 2,
		},
		{
			// 2-tab haystack line must NOT match a 1-tab needle.
			// Normalized hay line is "        hello" (8 sp); normalized needle
			// is "    hello" (4 sp); "    hello" does appear as substring at
			// offset 4 of "        hello", but offset 4 is not a line boundary.
			name:     "no false positive 2-tab vs 1-tab",
			hay:      "\t\thello\n",
			needle:   "\thello\n",
			wantHits: 0,
		},
		{
			// Mixed indentation in multi-line needle.
			// hay len=21; tab in "    x" expands to 4 sp in normalized form,
			// so end maps back to 21 (whole file).
			name:     "mixed tabs and spaces across lines",
			hay:      "func f() {\n\tx := 1\n}\n",
			needle:   "func f() {\n    x := 1\n}\n",
			wantS:    0,
			wantE:    21,
			wantHits: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e, hits := indentFlexibleFind(c.hay, c.needle)
			if hits != c.wantHits {
				t.Fatalf("hits: want %d, got %d (s=%d e=%d)", c.wantHits, hits, s, e)
			}
			if hits == 1 {
				if s != c.wantS || e != c.wantE {
					t.Errorf("offsets: want [%d,%d), got [%d,%d). hay[s:e]=%q", c.wantS, c.wantE, s, e, c.hay[s:e])
				}
			}
		})
	}
}

// Integration: full ResolveExternalPatches round-trip with tab/space mismatch.
func TestResolveSearchReplace_IndentFlexible_TabVsSpaces(t *testing.T) {
	root := t.TempDir()
	path := "indent.go"
	abs := filepath.Join(root, path)

	// File uses real tab indentation.
	original := "func f() {\n\treturn 1\n}\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// LLM supplied 4-space indented search block.
	got, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "func f() {\n    return 1\n}\n",
			Replace: "func f() {\n\treturn 2\n}\n",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].ReplaceRange == nil {
		t.Fatalf("expected 1 replace_range op, got %#v", got)
	}
	// Expected must contain verbatim bytes from file (real tabs).
	op := got[0].ReplaceRange
	if op.Expected != original {
		t.Errorf("Expected: want %q (verbatim file bytes), got %q", original, op.Expected)
	}
}

func TestResolveSearchReplace_IndentFlexible_AmbiguousFails(t *testing.T) {
	root := t.TempDir()
	path := "dup_indent.go"
	abs := filepath.Join(root, path)

	// Two identical tab-indented blocks; LLM uses spaces.
	original := "\thello\n\thello\n"
	if err := os.WriteFile(abs, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ResolveExternalPatches(root, []patches.Patch{
		{
			Type:    patches.TypeFileSearchReplace,
			Path:    path,
			Search:  "    hello\n",
			Replace: "    world\n",
		},
	})
	if err == nil {
		t.Fatal("expected AmbiguousMatch error, got nil")
	}
	pErr, ok := protocol.AsError(err)
	if !ok || pErr.Code != protocol.AmbiguousMatch {
		t.Fatalf("expected AmbiguousMatch, got %v", err)
	}
}

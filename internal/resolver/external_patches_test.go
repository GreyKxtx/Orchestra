package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/store"
)

func TestResolveExternalPatches_SearchReplace_ToOps_Apply(t *testing.T) {
	root := t.TempDir()
	path := "a.txt"
	abs := filepath.Join(root, path)

	before := "hello old world\n"
	if err := os.WriteFile(abs, []byte(before), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := store.ComputeSHA256([]byte(before))

	ops, err := ResolveExternalPatches(root, []externalpatch.Patch{
		{
			Type:     externalpatch.TypeFileSearchReplace,
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

	_, err := ResolveExternalPatches(root, []externalpatch.Patch{
		{
			Type:     externalpatch.TypeFileSearchReplace,
			Path:     path,
			Search:   "dup",
			Replace:  "x",
			FileHash: store.ComputeSHA256([]byte(before)),
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

	ops, err := ResolveExternalPatches(root, []externalpatch.Patch{
		{
			Type:     externalpatch.TypeFileUnifiedDiff,
			Path:     path,
			Diff:     diff,
			FileHash: store.ComputeSHA256([]byte(before)),
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

	ops, err := ResolveExternalPatches(root, []externalpatch.Patch{
		{
			Type:    externalpatch.TypeFileWriteAtomic,
			Path:    "new.txt",
			Content: "hello\n",
			Mode:    420,
			Conditions: &externalpatch.WriteAtomicConditions{
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

	_, err := ResolveExternalPatches(root, []externalpatch.Patch{
		{
			Type:    externalpatch.TypeFileWriteAtomic,
			Path:    "../evil.txt",
			Content: "nope",
			Conditions: &externalpatch.WriteAtomicConditions{
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

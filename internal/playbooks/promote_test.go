package playbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lessons"
)

func TestDraftFromLastPattern(t *testing.T) {
	root := t.TempDir()
	if err := lessons.Append(root, lessons.Entry{
		Dept:   "frontend",
		Kind:   lessons.KindPattern,
		Task:   "use vitest for ui pkg",
		Verify: "passed",
		Fix:    "run pnpm test",
	}); err != nil {
		t.Fatal(err)
	}
	rel, pending, err := DraftFromLastPattern(root, "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if rel != LocalOverlayRel("frontend") || !strings.HasPrefix(pending, "PENDING:") {
		t.Fatalf("rel=%q pending=%q", rel, pending)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "vitest") {
		t.Fatalf("body=%q", data)
	}
}

func TestMergeApprovedLocalToL2(t *testing.T) {
	root := t.TempDir()
	dept := "frontend"
	localRef := "approve vitest local overlay"
	mergeRef := "merge vitest rules to L2 playbook"
	localBody := "---\ndecision_ref: " + localRef + "\n---\n\n- Prefer vitest\n"
	localDir := filepath.Join(root, filepath.FromSlash(LocalRelDir))
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, dept+".md"), []byte(localBody), 0o644); err != nil {
		t.Fatal(err)
	}
	log := "- A: " + localRef + "\n- A: " + mergeRef + "\n"
	l2Rel, err := MergeApprovedLocalToL2(root, dept, mergeRef, log)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(l2Rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Prefer vitest") || !strings.Contains(string(data), mergeRef) {
		t.Fatalf("l2=%q", data)
	}
}

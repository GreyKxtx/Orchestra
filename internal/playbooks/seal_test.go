package playbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/lessons"
)

func TestSealPendingOverlayFromLog(t *testing.T) {
	root := t.TempDir()
	dept := "frontend"
	if err := lessons.Append(root, lessons.Entry{
		Dept: dept, Kind: lessons.KindPattern, Task: "use vitest", Verify: "passed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DraftFromLastPattern(root, dept); err != nil {
		t.Fatal(err)
	}
	log := "## 2026-08-15 · qa · frontend\n- Q: Approve local playbook overlay?\n- A: approve use vitest overlay\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := SealPendingOverlayFromLog(root, dept, log)
	if err != nil || !ok {
		t.Fatalf("seal: ok=%v err=%v", ok, err)
	}
	body, err := readLocalOverlayFile(root, dept)
	if err != nil {
		t.Fatal(err)
	}
	ref := ParseDecisionRef(body)
	if ref != "approve use vitest overlay" {
		t.Fatalf("decision_ref=%q", ref)
	}
	if !LocalOverlayApproved(body, log) {
		t.Fatal("expected approved overlay")
	}
}

func TestFormatPlaybookPromoteHintAfterSeal(t *testing.T) {
	root := t.TempDir()
	dept := "frontend"
	localRef := "approve vitest local overlay"
	localBody := "---\ndecision_ref: " + localRef + "\nstatus: approved\n---\n\n- Prefer vitest\n"
	dir := filepath.Join(root, filepath.FromSlash(LocalRelDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dept+".md"), []byte(localBody), 0o644); err != nil {
		t.Fatal(err)
	}
	log := "- A: " + localRef + "\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NeedsPlaybookPromoteHint(root, dept, localBody, log) {
		t.Fatal("expected promote hint needed")
	}
	hint := FormatPlaybookPromoteHint(dept)
	if !strings.Contains(hint, "playbook_promote") {
		t.Fatalf("hint=%q", hint)
	}
	depts := DeptsNeedingPromoteHint(root)
	if len(depts) != 1 || depts[0] != dept {
		t.Fatalf("depts=%v", depts)
	}
}

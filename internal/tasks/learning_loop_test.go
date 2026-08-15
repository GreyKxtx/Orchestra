package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/playbooks"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestLearningLoopPromoteSuggestions(t *testing.T) {
	root := t.TempDir()
	dept := "engineering"
	wo := &WorkOrder{
		Context: map[string]any{"scratchpad": ".orchestra/depts/engineering.md"},
		Intent:  "fix handler",
	}

	var hint string
	for i := 0; i < lessons.PromoteSuggestThreshold; i++ {
		hint = recordWorkerLesson(root, wo, nil, `{"status":"verification_failed"}`, "done")
	}
	if hint == "" || !strings.Contains(hint, "lesson_promote") {
		t.Fatalf("expected lesson_promote hint, got %q", hint)
	}
	out := annotateLessonPromoteSuggestion(`{"status":"verification_failed"}`, hint)
	if got := extractPromoteHintForTest(out); got == "" {
		t.Fatalf("missing lesson hint in %q", out)
	}

	if err := lessons.Append(root, lessons.Entry{
		Dept: dept, Kind: lessons.KindPattern, Task: wo.Intent, Verify: "passed", Fix: "run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := playbooks.DraftFromLastPattern(root, dept); err != nil {
		t.Fatal(err)
	}
	log := "## 2026-08-15 · qa\n- Q: Approve overlay?\n- A: approve engineering overlay\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if sealed := playbooks.TrySealAllPendingOverlays(root); len(sealed) != 1 {
		t.Fatalf("sealed=%v", sealed)
	}

	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	runner := &TaskRunner{toolRunner: tr}
	result := runner.attachPlaybookPromoteHints(`{"status":"ok"}`, wo)
	if got := extractPlaybookPromoteHintForTest(result); got == "" || !strings.Contains(got, "playbook_promote") {
		t.Fatalf("playbook hint=%q result=%q", got, result)
	}

	mergeRef := "merge engineering rules to L2"
	log += "- A: " + mergeRef + "\n"
	l2Rel, err := playbooks.MergeApprovedLocalToL2(root, dept, mergeRef, log)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(l2Rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "run tests") {
		t.Fatalf("l2=%q", data)
	}
}

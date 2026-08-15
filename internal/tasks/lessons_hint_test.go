package tasks

import (
	"testing"

	"github.com/orchestra/orchestra/internal/lessons"
)

func TestAnnotateLessonPromoteSuggestion(t *testing.T) {
	hint := lessons.FormatPromoteHint("frontend", lessons.PromoteSuggestThreshold)
	out := annotateLessonPromoteSuggestion(`{"status":"verification_failed"}`, hint)
	if got := extractPromoteHintForTest(out); got != hint {
		t.Fatalf("got %q", got)
	}
}

func TestAnnotatePlaybookPromoteSuggestion(t *testing.T) {
	hint := "playbook_promote hint"
	out := annotatePlaybookPromoteSuggestion(`{"status":"ok"}`, hint)
	if got := extractPlaybookPromoteHintForTest(out); got != hint {
		t.Fatalf("got %q", got)
	}
}

func TestRecordWorkerLessonPromoteHint(t *testing.T) {
	root := t.TempDir()
	wo := &WorkOrder{
		Context: map[string]any{"scratchpad": ".orchestra/depts/frontend.md"},
		Intent:  "fix ui test",
	}
	verify := "verification_failed: go test ./ui failed"
	for i := 0; i < lessons.PromoteSuggestThreshold-1; i++ {
		if hint := recordWorkerLesson(root, wo, nil, verify, "error"); hint != "" {
			t.Fatalf("unexpected hint at %d: %q", i+1, hint)
		}
	}
	if hint := recordWorkerLesson(root, wo, nil, verify, "error"); hint == "" {
		t.Fatal("expected promote hint on threshold")
	}
}

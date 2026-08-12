package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestFilterOpsByPaths(t *testing.T) {
	ops := []map[string]any{
		{"op": "file.write_atomic", "path": "a.go"},
		{"op": "file.write_atomic", "path": "b.go"},
	}
	got := filterOpsByPaths(ops, map[string]bool{"a.go": true})
	if len(got) != 1 {
		t.Fatalf("want 1 op, got %d", len(got))
	}
	if got[0]["path"] != "a.go" {
		t.Fatalf("wrong path: %v", got[0]["path"])
	}
}

func TestAcceptedDiffPathsFromSession(t *testing.T) {
	msgs := []state.Message{{
		Role: state.RoleDiff,
		DiffFiles: []state.DiffFile{
			{Path: "a.go", ReviewStatus: state.DiffReviewAccepted},
			{Path: "b.go", ReviewStatus: state.DiffReviewRejected},
			{Path: "c.go"},
		},
	}}
	paths := acceptedDiffPathsFromSession(msgs)
	if paths["a.go"] != true || paths["c.go"] != true {
		t.Fatalf("expected a.go and c.go accepted, got %#v", paths)
	}
	if paths["b.go"] {
		t.Fatal("rejected file must be excluded")
	}
}

func TestDiffReviewActive_requiresExpandedDiff(t *testing.T) {
	a := testChromeApp(t)
	a.session.AddDiffFiles([]state.DiffFile{{Path: "x.go", Before: "a", After: "b"}})
	if a.diffReviewActive() {
		t.Fatal("collapsed diff must not activate review hotkeys")
	}
	a.session.ExpandLastDiff()
	a.review.ResetCursor()
	a.syncDiffReviewCursor()
	if !a.diffReviewActive() {
		t.Fatal("expanded diff should activate review")
	}
}

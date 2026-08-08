package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestRenderDiffMessage_CollapsedOneLine(t *testing.T) {
	var beforeB strings.Builder
	var afterB strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&beforeB, "old line %d\n", i)
		fmt.Fprintf(&afterB, "new line %d\n", i)
	}
	out := RenderDiffMessage([]state.DiffFile{
		{Path: "big.txt", Before: beforeB.String(), After: afterB.String()},
	}, 80, false, -1)
	if strings.Contains(out, "скрыто") {
		t.Fatalf("collapsed should be one-line summary, not hidden-lines footer:\n%s", out)
	}
	if !strings.Contains(out, "Diff ") || !strings.Contains(out, "big.txt") {
		t.Fatalf("expected Diff path summary, got:\n%s", out)
	}
	if !strings.Contains(out, "+") || !strings.Contains(out, "−") && !strings.Contains(out, "-") {
		t.Fatalf("expected +/- counts, got:\n%s", out)
	}
	if !strings.Contains(out, "· d") {
		t.Fatalf("expected expand hint, got:\n%s", out)
	}
	// Collapsed must stay compact — no full body dump.
	if strings.Count(out, "\n") > 2 {
		t.Fatalf("collapsed diff too tall:\n%s", out)
	}
}

func TestRenderDiffMessage_Expanded(t *testing.T) {
	out := RenderDiffMessage([]state.DiffFile{
		{Path: "a.txt", Before: "a\n", After: "b\n"},
	}, 80, true, 0)
	if !strings.Contains(out, "▸") {
		t.Fatalf("expected cursor on first file:\n%s", out)
	}
	if !strings.Contains(out, "── a.txt ──") {
		t.Fatalf("expected path header:\n%s", out)
	}
	if !strings.Contains(out, "a/x") {
		t.Fatalf("expected review hint:\n%s", out)
	}
}

func TestShortDiffPath(t *testing.T) {
	long := "very/long/path/to/some/deeply/nested/component/FileName.tsx"
	got := shortDiffPath(long, DiffSummaryPathMax)
	if len([]rune(got)) > DiffSummaryPathMax {
		t.Fatalf("path too long: %q (%d)", got, len([]rune(got)))
	}
	if !strings.Contains(got, "FileName.tsx") && !strings.HasSuffix(got, "…") {
		t.Fatalf("expected basename or ellipsis, got %q", got)
	}
}

func TestCountDiffStats(t *testing.T) {
	add, rem := countDiffStats("a\nb\n", "a\nc\n")
	if add != 1 || rem != 1 {
		t.Fatalf("add=%d rem=%d want 1/1", add, rem)
	}
}

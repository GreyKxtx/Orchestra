package view_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestRenderFileDiff_AddedLine(t *testing.T) {
	out := view.RenderFileDiff("line1\nline2\n", "line1\nNEW\nline2\n", 80)
	if !strings.Contains(out, "+") {
		t.Fatalf("expected '+' marker in diff output:\n%s", out)
	}
}

func TestRenderFileDiff_RemovedLine(t *testing.T) {
	out := view.RenderFileDiff("line1\nOLD\nline2\n", "line1\nline2\n", 80)
	if !strings.Contains(out, "-") {
		t.Fatalf("expected '-' marker in diff output:\n%s", out)
	}
}

func TestRenderFileDiff_Identical(t *testing.T) {
	out := view.RenderFileDiff("same\n", "same\n", 80)
	if strings.Contains(out, "+") || strings.Contains(out, "-") {
		t.Fatalf("no +/- expected for identical files:\n%s", out)
	}
}

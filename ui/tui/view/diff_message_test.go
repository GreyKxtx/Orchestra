package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestRenderDiffMessage_Collapsed(t *testing.T) {
	var beforeB strings.Builder
	var afterB strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&beforeB, "old line %d\n", i)
		fmt.Fprintf(&afterB, "new line %d\n", i)
	}
	out := RenderDiffMessage([]state.DiffFile{
		{Path: "big.txt", Before: beforeB.String(), After: afterB.String()},
	}, 80, false)
	if !strings.Contains(out, "скрыто") {
		t.Fatalf("expected hidden line hint, got:\n%s", out)
	}
	if !strings.Contains(out, "Ctrl+T развернуть") {
		t.Fatalf("expected expand hint, got:\n%s", out)
	}
}

func TestRenderDiffMessage_Expanded(t *testing.T) {
	out := RenderDiffMessage([]state.DiffFile{
		{Path: "a.txt", Before: "a\n", After: "b\n"},
	}, 80, true)
	if strings.Contains(out, "скрыто") {
		t.Fatalf("expanded diff should not hide lines:\n%s", out)
	}
	if !strings.Contains(out, "── a.txt ──") {
		t.Fatalf("expected path header:\n%s", out)
	}
}

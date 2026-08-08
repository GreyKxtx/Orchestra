package view_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// TestChat_ActionBarSnapshot verifies inline action bar appears under the diff row.
func TestChat_ActionBarSnapshot(t *testing.T) {
	c := view.NewChat(80, 12)
	c.SetActionBar(view.ActionBarState{OpCount: 2, FileCount: 1, Review: true})
	c.SetMessages([]state.Message{{
		Role: state.RoleDiff,
		DiffFiles: []state.DiffFile{{
			Path:   "internal/foo.go",
			Before: "old\n",
			After:  "new\n",
		}},
	}})
	out := stripANSI(c.View())
	lines := nonEmptyLines(out)
	if len(lines) < 2 {
		t.Fatalf("expected diff + action bar lines, got %q", out)
	}
	if !strings.Contains(lines[0], "Diff internal/foo.go") {
		t.Fatalf("first line: %q", lines[0])
	}
	if !strings.Contains(lines[1], "pending") || !strings.Contains(lines[1], "discard") {
		t.Fatalf("action bar line: %q", lines[1])
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return out
}

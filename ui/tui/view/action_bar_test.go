package view_test

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestRenderActionBar_collapsedReview(t *testing.T) {
	out := view.RenderActionBar(view.ActionBarState{
		OpCount:   3,
		FileCount: 2,
		Review:    true,
	}, 80)
	if !strings.Contains(out, "ops") && !strings.Contains(out, "pending") {
		t.Fatalf("missing pending label: %q", out)
	}
	if !strings.Contains(out, "pply") || !strings.Contains(out, "discard") {
		t.Fatalf("missing hints: %q", out)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "3 ops") {
		t.Fatalf("unexpected plain output: %q", plain)
	}
}

func TestRenderActionBar_expandedReview(t *testing.T) {
	out := view.RenderActionBar(view.ActionBarState{
		OpCount:   1,
		FileCount: 1,
		Review:    true,
		Expanded:  true,
	}, 80)
	if !strings.Contains(out, "accept") {
		t.Fatalf("expected expanded hints: %q", out)
	}
	plain := stripANSI(out)
	if plain == "" {
		t.Fatal("empty output")
	}
}

// stripANSI removes escape sequences so golden files stay stable across terminals.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i+1 < len(s) && s[i+1] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

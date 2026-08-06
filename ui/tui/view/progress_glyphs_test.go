package view

import "testing"

func TestProgressGlyphs_SharedVocabulary(t *testing.T) {
	if ProgressPending != "○" || ProgressRunning != "⋯" || ProgressDone != "✓" {
		t.Fatalf("unexpected progress glyphs")
	}
	p := NewTaskPanel(60)
	p.SetOpen(true)
	p.SetItems([]TodoView{
		{Content: "a", Status: "pending"},
		{Content: "b", Status: "in_progress"},
		{Content: "c", Status: "done"},
	})
	out := p.RenderAboveInput()
	for _, g := range []string{ProgressPending, ProgressRunning, ProgressDone} {
		if !containsStr(out, g) {
			t.Fatalf("task panel missing glyph %q in %q", g, out)
		}
	}
}

func TestIdleChromeHints(t *testing.T) {
	// Idle status bar intentionally has no permanent key legend.
	sb := &StatusBar{width: 80}
	sb.SetProject("demo")
	sb.SetHints("")
	out := sb.Render()
	if containsStr(out, "Ctrl+K") || containsStr(out, "Ctrl+T") {
		t.Fatalf("idle bar should not show key legend, got: %q", out)
	}
}

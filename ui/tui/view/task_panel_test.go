package view

import "testing"

func TestTaskCounts(t *testing.T) {
	done, total := TaskCounts([]TodoView{
		{Status: "done"},
		{Status: "pending"},
		{Status: "cancelled"},
		{Status: "in_progress"},
	})
	if done != 1 || total != 3 {
		t.Fatalf("got done=%d total=%d, want 1/3", done, total)
	}
}

func TestTaskPanel_StripCollapsed(t *testing.T) {
	p := NewTaskPanel(80)
	p.SetItems([]TodoView{{Content: "a", Status: "pending"}})
	if p.VisibleRowsAboveInput() != 1 {
		t.Fatalf("strip should be 1 row, got %d", p.VisibleRowsAboveInput())
	}
	out := p.RenderAboveInput()
	if !containsAll(out, "Tasks", "Ctrl+T") {
		t.Fatalf("unexpected strip: %q", out)
	}
}

func TestTaskPanel_ExpandedScroll(t *testing.T) {
	p := NewTaskPanel(80)
	p.SetOpen(true)
	items := make([]TodoView, 8)
	for i := range items {
		items[i] = TodoView{Content: "task", Status: "pending"}
	}
	p.SetItems(items)
	if p.VisibleRowsAboveInput() != 5 {
		t.Fatalf("want 5 visible rows (1 header + 4 tasks), got %d", p.VisibleRowsAboveInput())
	}
	p.ScrollDown()
	out := p.RenderAboveInput()
	if out == "" {
		t.Fatal("expected expanded render")
	}
	if !containsAll(out, "2.", "○") {
		t.Fatalf("expected numbered pending row after scroll, got: %q", out)
	}
}

func TestTaskPanel_StatusGlyphs(t *testing.T) {
	p := NewTaskPanel(80)
	p.SetOpen(true)
	p.SetItems([]TodoView{
		{Content: "wait", Status: "pending"},
		{Content: "run", Status: "in_progress"},
		{Content: "ok", Status: "done"},
	})
	out := p.RenderAboveInput()
	if !containsAll(out, "1.", "2.", "3.", "○", "⋯", "✓") {
		t.Fatalf("expected numbered rows with status glyphs, got: %q", out)
	}
}

func TestStatusBar_ContextOverLimit(t *testing.T) {
	sb := &StatusBar{width: 120}
	sb.SetProject("smoke-demo")
	sb.SetModelCtx(60000)
	sb.SetTokens(810800, 60000)
	sb.SetLSPStatus("idle")
	out := sb.Render()
	if !containsAll(out, "810.8k/60.0k", "1351%") {
		t.Fatalf("expected real over-limit pct, got: %q", out)
	}
}

func TestStatusBar_BusyLayout(t *testing.T) {
	sb := &StatusBar{width: 120}
	sb.SetProject("smoke-demo")
	sb.SetAgentBusy(true)
	sb.SetModelCtx(60000)
	sb.SetTokens(1000, 60000)
	sb.SetLSPStatus("idle")
	out := sb.Render()
	if !containsAll(out, "smoke-demo", "Думаю", "1.0k/60.0k", "LSP") {
		t.Fatalf("busy bar should show project + metrics: %q", out)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsStr(s, p) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

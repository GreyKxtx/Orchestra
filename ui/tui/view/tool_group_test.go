package view

import (
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestSortToolsForDisplay_RunningLast(t *testing.T) {
	start := time.Now().Add(-5 * time.Second)
	blocks := []state.ToolBlock{
		{ID: "1", Name: "todoread", Status: state.ToolBlockRunning, StartedAt: start},
		{ID: "2", Name: "read", Status: state.ToolBlockCompleted, StartedAt: start, Duration: 2 * time.Second},
		{ID: "3", Name: "grep", Status: state.ToolBlockCompleted, StartedAt: start, Duration: 3 * time.Second},
	}
	got := sortToolsForDisplay(blocks)
	if got[0].Name != "read" || got[1].Name != "grep" || got[2].Name != "todoread" {
		t.Fatalf("order=%v,%v,%v", got[0].Name, got[1].Name, got[2].Name)
	}
}

func TestGroupElapsed_SingleToolUsesOwnDuration(t *testing.T) {
	d := 1500 * time.Millisecond
	blocks := []state.ToolBlock{
		{Name: "read", Status: state.ToolBlockCompleted, Duration: d},
	}
	if got := groupElapsed(blocks); got != d {
		t.Fatalf("got %v want %v", got, d)
	}
}

func TestRenderToolGroup_FooterUsesGroupNotTurn(t *testing.T) {
	start := time.Now().Add(-3 * time.Second)
	c := NewChat(80, 24)
	out := c.renderToolGroup([]state.ToolBlock{
		{Name: "read", Status: state.ToolBlockCompleted, StartedAt: start, Duration: 800 * time.Millisecond},
	}, 80, false, false, 5*time.Minute, "", "")
	if strings.Contains(out, "5m") {
		t.Fatalf("footer must not use turn duration: %q", out)
	}
	if !strings.Contains(out, "800ms") && !strings.Contains(out, "0.8s") {
		t.Fatalf("expected per-group/tool timing in output: %q", out)
	}
}

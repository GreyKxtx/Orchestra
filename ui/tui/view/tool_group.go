package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// renderToolGroup renders the model's tool calls as a compact inline list.
//
// Collapsed (default): one line per tool — `<icon> <name> <path/args> · <dur>`.
// Running tools render last so the active spinner sits at the bottom of the
// group (most recent activity). Footer shows this group's wall time, not the
// whole turn.
func (c Chat) renderToolGroup(blocks []state.ToolBlock, width int, expanded, streaming bool, _ time.Duration, title, mode string) string {
	_ = title
	_ = mode
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	display := sortToolsForDisplay(filterTodoWriteTools(blocks))
	if len(display) == 0 {
		return ""
	}

	failed, skipped := 0, 0
	for i := range display {
		switch display[i].Status {
		case state.ToolBlockFailed:
			failed++
		case state.ToolBlockSkipped:
			skipped++
		}
	}

	var lines []string
	for _, tb := range display {
		if expanded && isBlockStyleTool(tb, streaming) {
			tb2 := tb
			tb2.Expanded = true
			lines = append(lines, renderBlockTool(tb2, width, c.spinFrame))
		} else {
			lines = append(lines, renderInlineTool(tb, width, c.spinFrame))
		}
	}

	count := len(display)
	plural := "toolcalls"
	if count == 1 {
		plural = "toolcall"
	}
	footerText := fmt.Sprintf("└ %d %s", count, plural)
	if groupDur := groupElapsed(display); groupDur > 0 {
		footerText += " · " + formatDuration(groupDur)
	}
	if failed > 0 {
		footerText += fmt.Sprintf(" · %d failed", failed)
	}
	if skipped > 0 {
		footerText += fmt.Sprintf(" · %d skipped", skipped)
	}
	lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(muted.Render(footerText)))

	if !expanded && !streaming {
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(muted.Render("ctrl+t")))
	}

	return strings.Join(lines, "\n")
}

// filterTodoWriteTools hides todowrite from the tool list — the checklist
// SegmentTodos already shows the same data in-chat (Claude Code style).
func filterTodoWriteTools(blocks []state.ToolBlock) []state.ToolBlock {
	out := make([]state.ToolBlock, 0, len(blocks))
	for _, tb := range blocks {
		if toolKind(tb.Name) == "task" {
			continue
		}
		out = append(out, tb)
	}
	return out
}

// sortToolsForDisplay keeps chronological order but moves running tools to the
// end so the live spinner is always at the bottom of the group.
func sortToolsForDisplay(blocks []state.ToolBlock) []state.ToolBlock {
	out := append([]state.ToolBlock(nil), blocks...)
	sort.SliceStable(out, func(i, j int) bool {
		ri := out[i].Status == state.ToolBlockRunning
		rj := out[j].Status == state.ToolBlockRunning
		if ri != rj {
			return !ri
		}
		return false
	})
	return out
}

// toolElapsed returns per-tool wall time (live for running, stamped when done).
func toolElapsed(tb state.ToolBlock) time.Duration {
	if tb.Duration > 0 {
		return tb.Duration
	}
	if tb.Status == state.ToolBlockRunning && !tb.StartedAt.IsZero() {
		return time.Since(tb.StartedAt)
	}
	return 0
}

// groupElapsed is the wall-clock span of one tool segment (not the whole turn).
func groupElapsed(blocks []state.ToolBlock) time.Duration {
	if len(blocks) == 0 {
		return 0
	}
	if len(blocks) == 1 {
		return toolElapsed(blocks[0])
	}
	var first time.Time
	var lastEnd time.Time
	anyRunning := false
	for _, tb := range blocks {
		if !tb.StartedAt.IsZero() && (first.IsZero() || tb.StartedAt.Before(first)) {
			first = tb.StartedAt
		}
		if tb.Status == state.ToolBlockRunning {
			anyRunning = true
		}
		d := toolElapsed(tb)
		if !tb.StartedAt.IsZero() && d > 0 {
			end := tb.StartedAt.Add(d)
			if end.After(lastEnd) {
				lastEnd = end
			}
		}
	}
	if anyRunning && !first.IsZero() {
		return time.Since(first)
	}
	if !first.IsZero() && !lastEnd.IsZero() {
		return lastEnd.Sub(first)
	}
	return 0
}

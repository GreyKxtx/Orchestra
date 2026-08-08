package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// RenderTodosChecklist renders an in-chat / sticky task list (Claude Code style):
//
//	* Working…
//	  ■ current task
//	  □ pending task
//	  … +N pending, M completed
func RenderTodosChecklist(items []state.TodoItem, width int, streaming bool, spinFrame int) string {
	return RenderTodosChecklistCapped(items, width, streaming, spinFrame, 0)
}

// RenderTodosChecklistCapped is RenderTodosChecklist with an optional max visible
// task rows (0 = unlimited). Extra open tasks fold into the footer count.
func RenderTodosChecklistCapped(items []state.TodoItem, width int, streaming bool, spinFrame, maxRows int) string {
	if len(items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	warn := lipgloss.NewStyle().Foreground(t.Warning())
	ok := lipgloss.NewStyle().Foreground(t.Success())
	text := lipgloss.NewStyle().Foreground(t.Text())

	pending, inProg, done := 0, 0, 0
	var visible []state.TodoItem
	for _, it := range items {
		st := strings.ToLower(strings.TrimSpace(it.Status))
		switch st {
		case "done":
			done++
		case "in_progress":
			inProg++
			visible = append(visible, it)
		case "cancelled":
			// skip
		default:
			pending++
			visible = append(visible, it)
		}
	}

	var lines []string
	lead := muted.Render("* Tasks")
	if streaming && inProg > 0 {
		spin := lipgloss.NewStyle().Foreground(t.Primary()).Render(SpinnerFrames[spinFrame%len(SpinnerFrames)])
		lead = spin + " " + muted.Italic(true).Render("Working…")
	} else if streaming {
		spin := lipgloss.NewStyle().Foreground(t.Primary()).Render(SpinnerFrames[spinFrame%len(SpinnerFrames)])
		lead = spin + " " + muted.Italic(true).Render("Updating tasks…")
	}
	lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(lead))

	inner := width - 6
	if inner < 20 {
		inner = 20
	}
	shown := visible
	hidden := 0
	if maxRows > 0 && len(shown) > maxRows {
		hidden = len(shown) - maxRows
		shown = shown[:maxRows]
	}
	for _, it := range shown {
		st := strings.ToLower(strings.TrimSpace(it.Status))
		glyph, style := checklistGlyph(st, warn, ok, muted, spinFrame, streaming && st == "in_progress")
		content := strings.TrimSpace(it.Content)
		if content == "" {
			content = it.ID
		}
		content = truncRunes(content, inner)
		row := "  " + style.Render(glyph) + " " + text.Render(content)
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(row))
	}

	openLeft := pending + inProg
	footer := fmt.Sprintf("… +%d pending, %d completed", openLeft, done)
	if openLeft == 0 && done > 0 {
		footer = fmt.Sprintf("… %d completed", done)
	}
	if hidden > 0 {
		footer = fmt.Sprintf("… +%d more · %d pending, %d completed", hidden, openLeft, done)
	}
	lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(muted.Render(footer)))
	return strings.Join(lines, "\n")
}

func checklistGlyph(status string, warn, ok, muted lipgloss.Style, spinFrame int, spinning bool) (string, lipgloss.Style) {
	if spinning {
		return SpinnerFrames[spinFrame%len(SpinnerFrames)], lipgloss.NewStyle().Foreground(theme.CurrentTheme().Primary())
	}
	switch status {
	case "done":
		return "■", ok
	case "in_progress":
		return "■", warn
	default:
		return "□", muted
	}
}

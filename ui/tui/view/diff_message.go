package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// DefaultCollapsedDiffLines is the max rendered diff lines before collapse.
const DefaultCollapsedDiffLines = 50

// RenderDiffMessage renders a file diff panel with optional line cap (OpenCode-style).
func RenderDiffMessage(files []state.DiffFile, width int, expanded bool) string {
	_, _, ctxStyle, _ := diffStyles()
	if len(files) == 0 {
		return ctxStyle.Render("(нет изменений в файлах)")
	}
	views := make([]FileDiffView, len(files))
	for i, f := range files {
		views[i] = FileDiffView{Path: f.Path, Before: f.Before, After: f.After}
	}
	return renderDiffViews(views, width, expanded)
}

func renderDiffViews(files []FileDiffView, width int, expanded bool) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	headerStyle := lipgloss.NewStyle().Foreground(t.TextMuted()).Bold(true)
	_, _, _, pathStyle := diffStyles()

	var bodyLines []string
	totalLines := 0
	for i, fd := range files {
		if i > 0 {
			bodyLines = append(bodyLines, "")
			totalLines++
		}
		bodyLines = append(bodyLines, pathStyle.Render(fmt.Sprintf("── %s ──", fd.Path)))
		totalLines++
		for _, line := range strings.Split(RenderFileDiff(fd.Before, fd.After, width), "\n") {
			if line == "" {
				continue
			}
			bodyLines = append(bodyLines, line)
			totalLines++
		}
	}

	maxLines := DefaultCollapsedDiffLines
	if expanded {
		maxLines = len(bodyLines)
	}

	shown := bodyLines
	hidden := 0
	if len(bodyLines) > maxLines {
		shown = bodyLines[:maxLines]
		hidden = len(bodyLines) - maxLines
	}

	var out strings.Builder
	out.WriteString(headerStyle.Render("▣ Diff"))
	out.WriteString("\n")
	for _, line := range shown {
		out.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(line))
		out.WriteString("\n")
	}

	footer := fmt.Sprintf("└ %d строк", totalLines)
	if hidden > 0 {
		footer += fmt.Sprintf(" · … %d скрыто", hidden)
	}
	if !expanded && hidden > 0 {
		footer += " · Ctrl+T развернуть"
	} else if expanded && totalLines > DefaultCollapsedDiffLines {
		footer += " · Ctrl+T свернуть"
	}
	out.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(muted.Render(footer)))
	return strings.TrimRight(out.String(), "\n")
}

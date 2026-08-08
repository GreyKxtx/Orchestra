package view

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// DiffSummaryPathMax is the max visible runes for a file path in the collapsed
// one-line Diff summary (keeps the chat from flooding with long paths).
const DiffSummaryPathMax = 45

// RenderDiffMessage renders a file diff panel with optional line cap (OpenCode-style).
// cursor is the selected file index when expanded (-1 = no selection highlight).
func RenderDiffMessage(files []state.DiffFile, width int, expanded bool, cursor int) string {
	_, _, ctxStyle, _ := diffStyles()
	if len(files) == 0 {
		return ctxStyle.Render("(нет изменений в файлах)")
	}
	views := make([]FileDiffView, len(files))
	for i, f := range files {
		views[i] = FileDiffView{
			Path:         f.Path,
			Before:       f.Before,
			After:        f.After,
			ReviewStatus: f.ReviewStatus,
			Selected:     expanded && i == cursor,
		}
	}
	return renderDiffViews(views, width, expanded)
}

func renderDiffViews(files []FileDiffView, width int, expanded bool) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	headerStyle := lipgloss.NewStyle().Foreground(t.TextMuted()).Bold(true)
	addStyle, delStyle, _, pathStyle := diffStyles()
	okStyle := lipgloss.NewStyle().Foreground(t.Success())
	badStyle := lipgloss.NewStyle().Foreground(t.Error())
	selStyle := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true)

	if !expanded {
		return renderDiffSummary(files, width, headerStyle, pathStyle, addStyle, delStyle, muted, okStyle, badStyle)
	}

	var bodyLines []string
	totalLines := 0
	for i, fd := range files {
		if i > 0 {
			bodyLines = append(bodyLines, "")
			totalLines++
		}
		add, rem := countDiffStats(fd.Before, fd.After)
		prefix := "── "
		if fd.Selected {
			prefix = selStyle.Render("▸ ") + "── "
		}
		header := prefix + fd.Path + " ──"
		if fd.ReviewStatus == state.DiffReviewAccepted {
			header += okStyle.Render("  ✓")
		} else if fd.ReviewStatus == state.DiffReviewRejected {
			header += badStyle.Render("  ✗")
		}
		bodyLines = append(bodyLines, pathStyle.Render(header)+
			muted.Render(fmt.Sprintf("  +%d −%d", add, rem)))
		totalLines++
		if fd.ReviewStatus == state.DiffReviewRejected {
			bodyLines = append(bodyLines, muted.Render("(отклонено — diff скрыт)"))
			totalLines++
			continue
		}
		for _, line := range strings.Split(RenderFileDiff(fd.Before, fd.After, width), "\n") {
			if line == "" {
				continue
			}
			bodyLines = append(bodyLines, line)
			totalLines++
		}
	}

	var out strings.Builder
	out.WriteString(headerStyle.Render("▣ Diff"))
	out.WriteString(muted.Render(" · ↑↓ файл · a/x · Enter · d свернуть"))
	out.WriteString("\n")
	for _, line := range bodyLines {
		out.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(line))
		out.WriteString("\n")
	}
	out.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(muted.Render(fmt.Sprintf("└ %d строк", totalLines))))
	return strings.TrimRight(out.String(), "\n")
}

// renderDiffSummary is the collapsed view: one line per file —
// "Diff path… +N −M" with path truncated to DiffSummaryPathMax.
func renderDiffSummary(files []FileDiffView, width int, header, pathStyle, addStyle, delStyle, muted, okStyle, badStyle lipgloss.Style) string {
	var lines []string
	for i, fd := range files {
		add, rem := countDiffStats(fd.Before, fd.After)
		name := shortDiffPath(fd.Path, DiffSummaryPathMax)
		prefix := "Diff "
		if i == 0 {
			prefix = header.Render("Diff ")
		} else {
			prefix = muted.Render("Diff ")
		}
		line := prefix +
			pathStyle.Render(name) +
			" " + addStyle.Render(fmt.Sprintf("+%d", add)) +
			" " + delStyle.Render(fmt.Sprintf("−%d", rem))
		switch fd.ReviewStatus {
		case state.DiffReviewAccepted:
			line += okStyle.Render(" ✓")
		case state.DiffReviewRejected:
			line += badStyle.Render(" ✗")
		}
		if i == len(files)-1 {
			line += muted.Render("  · d")
		}
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(line))
	}
	_ = width
	return strings.Join(lines, "\n")
}

func shortDiffPath(path string, maxR int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "?"
	}
	base := filepath.ToSlash(path)
	if len([]rune(base)) <= maxR {
		return base
	}
	baseName := filepath.Base(filepath.FromSlash(base))
	if len([]rune(baseName)) >= maxR-1 {
		return truncRunes(baseName, maxR)
	}
	return truncRunes(base, maxR)
}

// countDiffStats returns added/removed line counts (ignores context).
func countDiffStats(before, after string) (added, removed int) {
	aLines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(after, "\n"), "\n")
	if before == "" {
		aLines = nil
	}
	if after == "" {
		bLines = nil
	}
	for _, l := range computeDiff(aLines, bLines) {
		switch l.kind {
		case lineAdded:
			added++
		case lineRemoved:
			removed++
		}
	}
	return added, removed
}

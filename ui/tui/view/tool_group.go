package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// renderToolGroup renders the model's tool calls as a compact inline list.
//
// Collapsed (default): one line per tool — `<icon> <name> <path/args>`. A
// muted footer summarises count and duration; a tiny ctrl+t hint reminds the
// user how to expand.
//
// Expanded (Ctrl+T): same list, but exec/write tools render as a left-bar
// detail block (command + truncated output). Other tools stay inline since
// their result is already summarised in the preview (entries/matches count).
//
// turnDur: elapsed for the current turn (0 = unknown, hides the suffix).
// title/mode: kept on the signature for compatibility; no longer rendered as
// a "Build Task — query" header because the request is already shown in the
// preceding user-message panel above.
func (c Chat) renderToolGroup(blocks []state.ToolBlock, width int, expanded, streaming bool, turnDur time.Duration, title, mode string) string {
	_ = title
	_ = mode
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	failed, skipped := 0, 0
	for i := range blocks {
		switch blocks[i].Status {
		case state.ToolBlockFailed:
			failed++
		case state.ToolBlockSkipped:
			skipped++
		}
	}

	var lines []string
	for _, tb := range blocks {
		if expanded && isBlockStyleTool(tb, streaming) {
			lines = append(lines, renderBlockTool(tb, width, c.spinFrame))
		} else {
			lines = append(lines, renderInlineTool(tb, width, c.spinFrame))
		}
	}

	count := len(blocks)
	plural := "toolcalls"
	if count == 1 {
		plural = "toolcall"
	}
	footerText := fmt.Sprintf("└ %d %s", count, plural)
	if turnDur > 0 {
		footerText += " · " + formatDuration(turnDur)
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


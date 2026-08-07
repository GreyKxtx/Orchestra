package view

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// renderInlineTool — single-line `<icon> <preview>` indented to col 3.
// Mirrors OpenCode's InlineTool: muted when complete, white when running,
// red when failed. While running, the static icon is replaced with the
// braille spinner glyph so the user sees motion on every tick.
//
// width: total available cell width including the col-3 indent. The label is
// clipped only when it would actually exceed the budget — short previews
// stay at their natural length, no full-width strip.
func renderInlineTool(tb state.ToolBlock, width, spinFrame int) string {
	t := theme.CurrentTheme()

	var iconColor, textColor lipgloss.Color
	skipped := false
	switch tb.Status {
	case state.ToolBlockRunning:
		iconColor = t.Primary()
		textColor = t.Text()
	case state.ToolBlockFailed:
		iconColor = t.Error()
		textColor = t.Error()
	case state.ToolBlockSkipped:
		iconColor = t.TextMuted()
		textColor = t.TextMuted()
		skipped = true
	default:
		iconColor = t.TextMuted()
		textColor = t.TextMuted()
	}

	label := toolPreview(tb.Name, tb.ArgsRaw, tb.Result)
	if suffix := diagnosticsInlineSuffix(tb.Diagnostics); suffix != "" {
		label += suffix
	}
	if dur := toolElapsed(tb); dur > 0 {
		label += lipgloss.NewStyle().Foreground(t.TextMuted()).Render(" · " + formatDuration(dur))
	}
	if label == "" {
		label = toolDisplayName(tb.Name)
	}

	iconCh := toolIcon(tb.Name)
	if tb.Status == state.ToolBlockRunning {
		iconCh = SpinnerFrames[spinFrame%len(SpinnerFrames)]
	}

	// LSP errors: highlight inline tool line (OpenCode-style soft validation feedback).
	if errs := countDiagErrors(tb.Diagnostics); errs > 0 && tb.Status == state.ToolBlockCompleted {
		iconColor = t.Error()
		textColor = t.Error()
	} else if countDiagWarnings(tb.Diagnostics) > 0 && tb.Status == state.ToolBlockCompleted {
		iconColor = t.Warning()
		textColor = t.Warning()
	}

	// Clip only when the label would actually overflow the row budget;
	// shorter labels render at natural width with no padding strip.
	if width > 0 {
		maxLabel := width - 5 // 3 indent + 1 icon + 1 space
		if maxLabel > 0 && lipgloss.Width(label) > maxLabel {
			label = clipLabel(label, maxLabel)
		}
	}

	icon := lipgloss.NewStyle().Foreground(iconColor).Render(iconCh)
	bodyStyle := lipgloss.NewStyle().Foreground(textColor)
	if skipped {
		bodyStyle = bodyStyle.Strikethrough(true).Faint(true)
	}
	body := bodyStyle.Render(label)
	return lipgloss.NewStyle().PaddingLeft(3).Render(icon + " " + body)
}

// clipLabel returns s clipped to maxRunes runes with a trailing "…" when
// truncated. Operates on runes so multibyte chars don't get split mid-encoding.
func clipLabel(s string, maxRunes int) string {
	if maxRunes <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}

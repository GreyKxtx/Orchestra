package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// isBlockStyleTool decides whether a tool renders as a multi-line panel block
// (exec/write/edit: command + truncated output) vs a one-line inline summary
// (read/list/search/etc: icon + path + entries/matches counter).
//
// Block style is suppressed while the message is still streaming so completed
// exec/write don't pop into heavy panels mid-stream.
func isBlockStyleTool(tb state.ToolBlock, streaming bool) bool {
	if tb.Status == state.ToolBlockRunning {
		return false
	}
	if streaming {
		return false
	}
	switch tb.Name {
	case "exec.run", "bash", "fs.write", "file.write_atomic", "write", "edit", "Edit", "MultiEdit":
		return true
	}
	if toolKind(tb.Name) == "write" {
		return true
	}
	if toolKind(tb.Name) == "exec" {
		return true
	}
	return false
}

// renderBlockTool — compact tool detail box. No background fill; left ┃ bar
// only. Title is icon + preview (no "# " prefix). Body text is muted.
// Used only for completed tools when the user expands the group (Ctrl+T).
func renderBlockTool(tb state.ToolBlock, width int, spinFrame int) string {
	t := theme.CurrentTheme()

	preview := toolPreview(tb.Name, tb.ArgsRaw, tb.Result)
	if preview == "" {
		preview = toolDisplayName(tb.Name)
	}
	var titleText string
	if tb.Status == state.ToolBlockRunning {
		titleText = SpinnerFrames[spinFrame%len(SpinnerFrames)] + " " + preview
	} else {
		titleText = toolIcon(tb.Name) + " " + preview
	}

	body := blockToolBody(tb)

	const (
		padH   = 2
		barCol = 1
		indent = 2
	)
	maxInner := width - 2*padH - barCol - indent
	if maxInner < 10 {
		maxInner = 10
	}
	naturalW := maxLineWidth(titleText)
	if w := maxLineWidth(body); w > naturalW {
		naturalW = w
	}
	if naturalW > maxInner {
		naturalW = maxInner
	}
	panelW := naturalW + 2*padH

	title := lipgloss.NewStyle().Foreground(t.Text()).Render(titleText)
	bodyStyled := lipgloss.NewStyle().Foreground(t.TextMuted()).Render(body)

	// No background fill — plain left-border only.
	box := lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.TextMuted()).
		Padding(1, padH).
		Width(panelW).
		Render(title + "\n\n" + bodyStyled)
	return lipgloss.NewStyle().PaddingLeft(indent).Render(box)
}

// maxLineWidth returns the widest line in s, measured with lipgloss.Width
// (which counts visible runes, not bytes).
func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}

// blockToolBody builds the inner content of a BlockTool — per-tool layout.
// exec.run: `$ <command>` + output. fs.write/edit: new content. otherwise: result.
// When the tool (or group) is Expanded, show more lines before truncating.
func blockToolBody(tb state.ToolBlock) string {
	maxLines := 20
	if tb.Expanded {
		maxLines = 200
	}
	args := parseToolArgs(tb.ArgsRaw)
	switch toolKind(tb.Name) {
	case "exec":
		cmd, _ := args["command"].(string)
		var b strings.Builder
		if cmd != "" {
			b.WriteString("$ " + cmd)
		}
		out := strings.TrimSpace(tb.Result)
		if out != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(truncateLines(out, maxLines))
		}
		if b.Len() == 0 {
			return "(running…)"
		}
		return b.String()
	case "write":
		content, _ := args["content"].(string)
		if content == "" {
			content = strings.TrimSpace(tb.Result)
		}
		var b strings.Builder
		if content != "" {
			b.WriteString(truncateLines(content, maxLines))
		} else if len(tb.Diagnostics) == 0 {
			return "(running…)"
		}
		if diagBlock := formatDiagnosticsBlock(tb.Diagnostics); diagBlock != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(diagBlock)
		}
		if b.Len() == 0 {
			return "(running…)"
		}
		return b.String()
	default:
		out := strings.TrimSpace(tb.Result)
		if out == "" {
			return "(no output)"
		}
		return truncateLines(out, maxLines)
	}
}

package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// isBlockStyleTool decides whether a tool renders as a multi-line panel block
// (exec/write: command + truncated output) vs a one-line inline summary
// (read/list/search/etc: icon + path + entries/matches counter).
//
// Only exec.run / fs.write / file.write_atomic ever get the block style — their
// payload (command, file content) is the actual user-relevant artifact. For
// read-style tools the inline preview already conveys everything useful and a
// full body would just dump JSON noise into the chat.
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
	case "exec.run", "fs.write", "file.write_atomic":
		return true
	}
	return false
}

// renderBlockTool — compact tool detail box. No background fill; left ┃ bar
// only. Title is icon + preview (no "# " prefix). Body text is muted.
// Used only for completed tools when the user expands the group (Ctrl+T) or
// expands an individual tool via Tab.
//
// exec.run: body shows `$ <command>` followed by output (max 20 lines).
// fs.write: body shows the new file content (max 20 lines).
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
// exec.run: `$ <command>` + output. fs.write: new content. otherwise: result.
func blockToolBody(tb state.ToolBlock) string {
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
			b.WriteString(truncateLines(out, 20))
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
		if content == "" {
			return "(running…)"
		}
		return truncateLines(content, 20)
	default:
		out := strings.TrimSpace(tb.Result)
		if out == "" {
			return "(no output)"
		}
		return truncateLines(out, 20)
	}
}

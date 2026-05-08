package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

const appVersion = "v0.6"

// WelcomeInfo contains project metadata shown on the empty-state welcome screen.
type WelcomeInfo struct {
	ProjectPath  string // full path to workspace root
	ProjectName  string // display name (base of path)
	ModelName    string // configured model, or "" if none
	SessionCount int    // number of past sessions in this project
}

// Chat renders the scrollable history of messages.
type Chat struct {
	vp           viewport.Model
	streamCursor bool        // when true, appends ▋ to last assistant token
	welcome      WelcomeInfo // metadata for the empty-state screen
	forceWelcome bool        // when true, always show welcome regardless of content
}

// SetWelcomeInfo updates the project metadata displayed on the welcome screen.
func (c *Chat) SetWelcomeInfo(info WelcomeInfo) {
	c.welcome = info
}

// SetForceWelcome controls whether the welcome screen is shown regardless of chat content.
func (c *Chat) SetForceWelcome(v bool) {
	c.forceWelcome = v
}

// NewChat creates an empty chat view sized to width × height.
func NewChat(width, height int) Chat {
	return Chat{vp: viewport.New(width, height)}
}

// SetSize resizes the chat viewport.
func (c *Chat) SetSize(width, height int) {
	c.vp.Width = width
	c.vp.Height = height
}

// SetStreamCursor controls whether a blinking cursor is appended to
// the last message (used while agent is streaming a response).
func (c *Chat) SetStreamCursor(on bool) {
	c.streamCursor = on
}

// SetMessages re-renders the viewport content from the session messages.
// Any non-empty message list also dismisses the force-welcome overlay.
func (c *Chat) SetMessages(msgs []state.Message) {
	if len(msgs) == 0 {
		c.vp.SetContent("")
		return
	}
	// Dismiss the startup welcome as soon as real content appears.
	c.forceWelcome = false

	t := theme.CurrentTheme()
	toolStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	toolErrStyle := lipgloss.NewStyle().Foreground(t.Error())
	expandedHdr := lipgloss.NewStyle().Foreground(t.TextMuted())
	expandedBody := lipgloss.NewStyle().Foreground(t.Text())
	sysStyle := lipgloss.NewStyle().Foreground(t.Warning()).Italic(true)
	diffStyle := lipgloss.NewStyle().Foreground(t.TextMuted())

	width := c.vp.Width

	var b strings.Builder
	for i, m := range msgs {
		switch m.Role {
		case state.RoleUser:
			b.WriteString(renderMessage(m.Text, true, width, ""))

		case state.RoleAssistant:
			text := m.Text
			// Append streaming cursor to last assistant message.
			if c.streamCursor && i == len(msgs)-1 {
				text += "▋"
			}
			// Build tool block section.
			var toolLines strings.Builder
			for _, tb := range m.ToolBlocks {
				style := toolStyle
				if tb.Status == state.ToolBlockFailed {
					style = toolErrStyle
				}
				if tb.Expanded && tb.Status != state.ToolBlockRunning {
					toolLines.WriteString(expandedHdr.Render(fmt.Sprintf("▾ %s", tb.Name)))
					if tb.Result != "" {
						lines := strings.Split(tb.Result, "\n")
						const maxLines = 50
						shown := lines
						truncated := 0
						if len(lines) > maxLines {
							shown = lines[:maxLines]
							truncated = len(lines) - maxLines
						}
						for _, l := range shown {
							toolLines.WriteString("\n" + expandedBody.Render("  "+l))
						}
						if truncated > 0 {
							toolLines.WriteString("\n" + expandedBody.Render(fmt.Sprintf("  … %d more lines", truncated)))
						}
					}
				} else {
					marker := "▸"
					if tb.Status == state.ToolBlockRunning {
						marker = "⋯"
					}
					summary := fmt.Sprintf("%s %s", marker, tb.Name)
					if tb.Result != "" && tb.Status != state.ToolBlockRunning {
						preview := tb.Result
						if len(preview) > 80 {
							preview = preview[:80] + "…"
						}
						preview = strings.ReplaceAll(preview, "\n", " ")
						summary += " → " + preview
					}
					toolLines.WriteString(style.Render(summary))
				}
				toolLines.WriteString("\n")
			}
			b.WriteString(renderMessage(text, false, width, toolLines.String()))

		case state.RoleSystem:
			b.WriteString(sysStyle.Render(m.Text))

		case state.RoleDiff:
			b.WriteString(diffStyle.Render(m.Text))
		}

		if i < len(msgs)-1 {
			b.WriteString("\n\n")
		}
	}
	c.vp.SetContent(b.String())
	c.vp.GotoBottom()
}

// renderMessage renders a single message with a thick left border.
// isUser=true → yellow (Secondary), isUser=false → purple (Primary).
// extra is appended below content (e.g. tool blocks).
func renderMessage(text string, isUser bool, width int, extra string) string {
	t := theme.CurrentTheme()

	borderColor := t.Primary()
	if isUser {
		borderColor = t.Secondary()
	}

	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	rendered := renderMarkdown(text, innerWidth)
	content := rendered
	if extra != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, rendered, extra)
	}

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(borderColor).
		PaddingLeft(1).
		PaddingRight(1).
		Width(width - 2).
		Render(content)
}

// renderMarkdown renders text through glamour with dark styling.
func renderMarkdown(text string, width int) string {
	if width < 10 {
		width = 10
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSpace(out)
}

// welcomeScreen returns a centered welcome block shown when chat is empty.
func (c Chat) welcomeScreen() string {
	t := theme.CurrentTheme()
	w := c.vp.Width
	h := c.vp.Height

	const blockWidth = 44

	logoStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true)

	appNameStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted())

	labelStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted())

	valueStyle := lipgloss.NewStyle().
		Foreground(t.Text())

	warnStyle := lipgloss.NewStyle().
		Foreground(t.Warning())

	hintStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Italic(true)

	sep := subtitleStyle.Render(strings.Repeat("─", blockWidth))

	// Logo + name block
	note := logoStyle.Render("  ♪")
	name := appNameStyle.Render("  Orchestra Code")
	version := subtitleStyle.Render("  AI coding assistant  " + appVersion)

	// Project info
	projectPath := c.welcome.ProjectPath
	if projectPath == "" {
		projectPath = "."
	}
	projectName := c.welcome.ProjectName
	if projectName == "" {
		projectName = "unknown"
	}

	projectLine := labelStyle.Render("  📁  ") + valueStyle.Render(projectName) +
		subtitleStyle.Render("  "+projectPath)

	// Model info
	var modelLine string
	if c.welcome.ModelName == "" {
		modelLine = labelStyle.Render("  🤖  ") + warnStyle.Render("Модель не выбрана") +
			subtitleStyle.Render("  — нажми Ctrl+O")
	} else {
		modelLine = labelStyle.Render("  🤖  ") + valueStyle.Render(c.welcome.ModelName)
	}

	// Sessions
	sessionsText := fmt.Sprintf("%d", c.welcome.SessionCount)
	if c.welcome.SessionCount == 0 {
		sessionsText = "нет"
	}
	sessionsLine := labelStyle.Render("  💬  ") + valueStyle.Render(sessionsText+" сессий")

	// Start hint
	var hint string
	if c.welcome.ModelName == "" {
		hint = hintStyle.Render("  Настрой модель через Ctrl+O для начала работы")
	} else {
		hint = hintStyle.Render("  Напиши сообщение чтобы начать…")
	}

	block := lipgloss.JoinVertical(lipgloss.Left,
		note,
		name,
		version,
		"",
		sep,
		"",
		projectLine,
		modelLine,
		sessionsLine,
		"",
		sep,
		"",
		hint,
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
}

// View returns the viewport's current view, or the welcome screen if empty/forced.
func (c Chat) View() string {
	if c.vp.Width == 0 {
		return ""
	}
	if c.forceWelcome || c.vp.TotalLineCount() == 0 {
		return c.welcomeScreen()
	}
	return c.vp.View()
}

// Render is an alias for View (keeps compatibility with app.go).
func (c Chat) Render() string { return c.View() }

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
	agentBusy    bool        // affects the help line shown below messages
	width        int
	height       int
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
	// reserve 1 row at the bottom for the help line
	return Chat{vp: viewport.New(width, height-1), width: width, height: height}
}

// SetSize resizes the chat viewport (1 row reserved for help line).
func (c *Chat) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.vp.Width = width
	c.vp.Height = height - 1
}

// SetAgentBusy controls the help line text shown below messages.
func (c *Chat) SetAgentBusy(b bool) { c.agentBusy = b }

// SetStreamCursor controls whether a blinking cursor is appended to
// the last message (used while agent is streaming a response).
func (c *Chat) SetStreamCursor(on bool) {
	c.streamCursor = on
}

// SetMessages re-renders the viewport content from the session messages.
func (c *Chat) SetMessages(msgs []state.Message) {
	if len(msgs) == 0 {
		c.vp.SetContent("")
		return
	}

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

// ASCII art — Calvin S / box-drawing style.
// ORCHESTRA: O R C H E S T R A (3 wide per letter, 1 space gap)
const orchArt = "╔═╗ ╦═╗ ╔═╗ ╦ ╦ ╔═╗ ╔═╗ ╔╦╗ ╦═╗ ╔═╗\n" +
	"║ ║ ╠╦╝ ║   ╠═╣ ╠═  ╚═╗  ║  ╠╦╝ ╠═╣\n" +
	"╚═╝ ╩╚═ ╚═╝ ╩ ╩ ╚═╝ ╚═╝  ╩  ╩╚═ ╚ ╝"

// CODE: C O D E
const codeArt = "╔═╗ ╔═╗ ╔╦╗ ╔═╗\n" +
	"║   ║ ║ ║ ║ ╠═ \n" +
	"╚═╝ ╚═╝ ╚╩╝ ╚═╝"

// ThemeForApp exposes the current theme to app.go callers.
func ThemeForApp() theme.Theme { return theme.CurrentTheme() }

// RenderWelcomeLogo returns the colored ORCHESTRA + CODE block,
// vertically stacked. Used by the welcome view.
func RenderWelcomeLogo() string {
	t := theme.CurrentTheme()
	orch := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true).Render(orchArt)
	code := lipgloss.NewStyle().Foreground(t.Secondary()).Bold(true).Render(codeArt)
	return lipgloss.JoinVertical(lipgloss.Center, orch, code)
}

// welcomeScreen — port of OpenCode initialScreen+header+lspsConfigured.
// All elements are left-aligned at the top, full width. No centering.
func (c Chat) welcomeScreen() string {
	w := c.width
	if w == 0 {
		w = c.vp.Width
	}

	return lipgloss.NewStyle().Width(w).Render(
		lipgloss.JoinVertical(
			lipgloss.Top,
			c.welcomeLogo(w),
			c.welcomeVersion(w),
			"",
			c.welcomeCwd(w),
			"",
			c.welcomeProjectInfo(w),
		),
	)
}

// welcomeLogo renders the ORCHESTRA + CODE block, left-aligned, full width.
func (c Chat) welcomeLogo(width int) string {
	t := theme.CurrentTheme()
	orch := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true).Render(orchArt)
	code := lipgloss.NewStyle().Foreground(t.Secondary()).Bold(true).Render(codeArt)
	return lipgloss.NewStyle().Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left, orch, code),
	)
}

// welcomeVersion — single muted line "<version> · AI coding assistant".
func (c Chat) welcomeVersion(width int) string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(appVersion + " · AI coding assistant")
}

// welcomeCwd — "cwd: <path>" muted, full-width (mirrors OpenCode cwd()).
func (c Chat) welcomeCwd(width int) string {
	t := theme.CurrentTheme()
	path := c.welcome.ProjectPath
	if path == "" {
		path = "."
	}
	return lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render("cwd: " + path)
}

// welcomeProjectInfo — primary-bold title + bulleted lines (mirrors lspsConfigured).
func (c Chat) welcomeProjectInfo(width int) string {
	t := theme.CurrentTheme()
	base := lipgloss.NewStyle()

	title := base.
		Width(width).
		Foreground(t.Primary()).
		Bold(true).
		Render("Project")

	name := c.welcome.ProjectName
	if name == "" {
		name = "(unknown)"
	}
	nameLine := base.Width(width).Render(
		base.Foreground(t.Text()).Render("• ") +
			base.Foreground(t.Text()).Render(name),
	)

	var modelLine string
	if c.welcome.ModelName == "" {
		modelLine = base.Width(width).Render(
			base.Foreground(t.Text()).Render("• model: ") +
				base.Foreground(t.Warning()).Render("не выбрана ") +
				base.Foreground(t.TextMuted()).Render("(ctrl+o)"),
		)
	} else {
		modelLine = base.Width(width).Render(
			base.Foreground(t.Text()).Render("• model: ") +
				base.Foreground(t.Text()).Render(c.welcome.ModelName),
		)
	}

	sessLine := base.Width(width).Render(
		base.Foreground(t.Text()).Render("• ") +
			base.Foreground(t.Text()).Render(fmt.Sprintf("sessions: %d", c.welcome.SessionCount)),
	)

	return base.Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			nameLine,
			modelLine,
			sessLine,
		),
	)
}

// helpLine — bottom-of-messages help text, mirrors OpenCode help().
func (c Chat) helpLine() string {
	t := theme.CurrentTheme()
	base := lipgloss.NewStyle().Bold(true)

	var text string
	if c.agentBusy {
		text = lipgloss.JoinHorizontal(lipgloss.Left,
			base.Foreground(t.TextMuted()).Render("press "),
			base.Foreground(t.Text()).Render("esc"),
			base.Foreground(t.TextMuted()).Render(" to cancel"),
		)
	} else {
		text = lipgloss.JoinHorizontal(lipgloss.Left,
			base.Foreground(t.TextMuted()).Render("press "),
			base.Foreground(t.Text()).Render("enter"),
			base.Foreground(t.TextMuted()).Render(" to send the message,"),
			base.Foreground(t.TextMuted()).Render(" write"),
			base.Foreground(t.Text()).Render(" \\"),
			base.Foreground(t.TextMuted()).Render(" and enter to add a new line"),
		)
	}
	return lipgloss.NewStyle().Width(c.width).Render(text)
}

// View returns the viewport's current view, or the welcome screen if empty/forced.
// A help line is appended at the bottom in both cases.
func (c Chat) View() string {
	if c.vp.Width == 0 {
		return ""
	}
	var top string
	if c.forceWelcome || c.vp.TotalLineCount() == 0 {
		// Welcome takes height-1 rows, help line takes the last row.
		top = lipgloss.NewStyle().
			Width(c.width).
			Height(c.height - 1).
			Render(c.welcomeScreen())
	} else {
		top = c.vp.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, c.helpLine())
}

// Render is an alias for View (keeps compatibility with app.go).
func (c Chat) Render() string { return c.View() }

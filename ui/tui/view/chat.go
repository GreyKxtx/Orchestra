package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// Chat renders the scrollable history of messages.
type Chat struct {
	vp viewport.Model
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

// SetMessages re-renders the viewport content from the session messages.
func (c *Chat) SetMessages(msgs []state.Message) {
	var b strings.Builder
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	asstStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Italic(true)
	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	toolErrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))

	for i, m := range msgs {
		switch m.Role {
		case state.RoleUser:
			b.WriteString(userStyle.Render("> ") + m.Text)
		case state.RoleAssistant:
			if m.Text != "" {
				b.WriteString(asstStyle.Render(m.Text))
			}
			for _, tb := range m.ToolBlocks {
				if m.Text != "" || len(b.String()) > 0 {
					b.WriteString("\n")
				}
				style := toolStyle
				if tb.Status == state.ToolBlockFailed {
					style = toolErrStyle
				}
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
				b.WriteString(style.Render(summary))
			}
		case state.RoleSystem:
			b.WriteString(sysStyle.Render(m.Text))
		}
		if i < len(msgs)-1 {
			b.WriteString("\n\n")
		}
	}
	c.vp.SetContent(b.String())
	c.vp.GotoBottom()
}

// Render returns the viewport's current view.
func (c Chat) Render() string {
	return c.vp.View()
}

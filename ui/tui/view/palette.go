package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// SlashCmd is one slash command shown in the palette.
type SlashCmd struct {
	Cmd  string
	Desc string
}

// AllSlashCmds is the complete list shown in the slash palette.
var AllSlashCmds = []SlashCmd{
	{"/help", "show available commands"},
	{"/clear", "clear chat history"},
	{"/diff", "toggle diff view"},
	{"/apply", "apply pending ops"},
	{"/discard", "discard pending ops"},
	{"/model", "show current model"},
	{"/mode", "show current mode"},
	{"/quit", "exit Orchestra TUI"},
}

const maxPaletteVisible = 6

// SlashPalette renders a filtered list of slash commands.
type SlashPalette struct {
	Items  []SlashCmd
	Cursor int
	width  int
}

// NewSlashPalette creates a palette sized to the given width.
func NewSlashPalette(width int) *SlashPalette {
	return &SlashPalette{width: width}
}

// SetSize updates the rendering width.
func (p *SlashPalette) SetSize(width int) { p.width = width }

// Filter updates Items to commands containing the query (after the leading /).
func (p *SlashPalette) Filter(query string) {
	q := strings.ToLower(query)
	filtered := make([]SlashCmd, 0, len(AllSlashCmds))
	for _, c := range AllSlashCmds {
		if q == "" || strings.Contains(c.Cmd[1:], q) {
			filtered = append(filtered, c)
		}
	}
	p.Items = filtered
	if len(p.Items) > 0 && p.Cursor >= len(p.Items) {
		p.Cursor = len(p.Items) - 1
	} else if len(p.Items) == 0 {
		p.Cursor = 0
	}
}

// CursorUp moves the cursor toward the top, clamping at 0.
func (p *SlashPalette) CursorUp() {
	if p.Cursor > 0 {
		p.Cursor--
	}
}

// CursorDown moves the cursor toward the bottom, clamping at len-1.
func (p *SlashPalette) CursorDown() {
	if p.Cursor < len(p.Items)-1 {
		p.Cursor++
	}
}

// Selected returns the Cmd string of the highlighted item, or "" if empty.
func (p *SlashPalette) Selected() string {
	if len(p.Items) == 0 || p.Cursor >= len(p.Items) {
		return ""
	}
	return p.Items[p.Cursor].Cmd
}

// Render returns the palette in OpenCode's completion-dialog style:
// top-only NormalBorder in TextMuted, full input-width, theme-aware colors.
// Selected item: Primary bg, Background fg.
func (p *SlashPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	visible := p.Items
	if len(visible) > maxPaletteVisible {
		visible = visible[:maxPaletteVisible]
	}

	w := p.width
	if w < 20 {
		w = 20
	}
	innerW := w - 2 // accounts for 1-col padding on each side

	cmdW := 12
	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(innerW)
	cmdStyle := lipgloss.NewStyle().Foreground(t.Text()).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(t.TextMuted())

	var b strings.Builder
	for i, item := range visible {
		padCmd := fmt.Sprintf("%-*s", cmdW, item.Cmd)
		var line string
		if i == p.Cursor {
			line = selStyle.Render(" " + padCmd + " " + item.Desc)
		} else {
			line = " " + cmdStyle.Render(padCmd) + " " + descStyle.Render(item.Desc)
		}
		b.WriteString(line)
		if i < len(visible)-1 {
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(t.TextMuted()).
		BorderBackground(t.Background()).
		Width(w).
		Render(b.String())
}

// MentionPalette renders a filtered list of file paths for @-mention completion.
type MentionPalette struct {
	Items  []string
	Cursor int
	width  int
}

// NewMentionPalette creates a mention palette sized to the given width.
func NewMentionPalette(width int) *MentionPalette {
	return &MentionPalette{width: width}
}

// SetSize updates the rendering width.
func (p *MentionPalette) SetSize(width int) { p.width = width }

// SetItems replaces the item list and resets the cursor.
func (p *MentionPalette) SetItems(items []string) {
	p.Items = items
	if p.Cursor >= len(p.Items) {
		p.Cursor = 0
	}
}

// CursorUp moves the cursor toward the top, clamping at 0.
func (p *MentionPalette) CursorUp() {
	if p.Cursor > 0 {
		p.Cursor--
	}
}

// CursorDown moves the cursor toward the bottom, clamping at len-1.
func (p *MentionPalette) CursorDown() {
	if p.Cursor < len(p.Items)-1 {
		p.Cursor++
	}
}

// Selected returns the currently highlighted file path, or "".
func (p *MentionPalette) Selected() string {
	if len(p.Items) == 0 || p.Cursor >= len(p.Items) {
		return ""
	}
	return p.Items[p.Cursor]
}

// Render returns a styled popup listing visible file paths — same OpenCode
// completion style as SlashPalette.
func (p *MentionPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	visible := p.Items
	if len(visible) > maxPaletteVisible {
		visible = visible[:maxPaletteVisible]
	}

	w := p.width
	if w < 20 {
		w = 20
	}
	innerW := w - 2

	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(innerW)
	itemStyle := lipgloss.NewStyle().Foreground(t.Text())

	var b strings.Builder
	for i, item := range visible {
		var line string
		if i == p.Cursor {
			line = selStyle.Render(" " + item)
		} else {
			line = " " + itemStyle.Render(item)
		}
		b.WriteString(line)
		if i < len(visible)-1 {
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(t.TextMuted()).
		BorderBackground(t.Background()).
		Width(w).
		Render(b.String())
}

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
// Sorted alphabetically — opencode style.
var AllSlashCmds = []SlashCmd{
	{"/apply", "apply pending ops"},
	{"/clear", "clear chat history"},
	{"/diff", "toggle diff view"},
	{"/discard", "discard pending ops"},
	{"/help", "show available commands"},
	{"/mode", "show current mode"},
	{"/model", "show current model"},
	{"/quit", "exit Orchestra TUI"},
}

const maxPaletteVisible = 6

// splitBorder mimics opencode's SplitBorder: thick ┃ on left and right only,
// no top/bottom/corners. Reads as a discrete menu element rather than a
// continuation of the input box.
var splitBorder = lipgloss.Border{
	Left:  "┃",
	Right: "┃",
}

// paletteCursor encapsulates the cursor-up/down/clamp logic shared by every
// palette. The struct keeps Cursor exported so the existing tests that probe
// internal state continue to work.
type paletteCursor struct {
	Cursor int
}

func (p *paletteCursor) cursorUp() {
	if p.Cursor > 0 {
		p.Cursor--
	}
}

func (p *paletteCursor) cursorDown(max int) {
	if p.Cursor < max-1 {
		p.Cursor++
	}
}

// renderPaletteList returns a JoinVertical of one rendered row per visible
// item. rowFor returns the rendered, full-width row for item i (with cursor
// highlight applied when i == cursor). Caller is responsible for slicing
// visible items themselves before passing length.
func renderPaletteList(length int, cursor int, rowFor func(i int) string) string {
	if length <= 0 {
		return ""
	}
	if length > maxPaletteVisible {
		length = maxPaletteVisible
	}
	var b strings.Builder
	for i := 0; i < length; i++ {
		b.WriteString(rowFor(i))
		if i < length-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// paletteBox wraps inner content in the opencode SplitBorder shell. Returns
// the boxed string ready for placement above the input.
func paletteBox(inner string, width int) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	return lipgloss.NewStyle().
		Background(bg).
		Border(splitBorder, false, true, false, true).
		BorderForeground(t.Primary()).
		BorderBackground(bg).
		Padding(0, 1).
		Width(width).
		Render(inner)
}

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

// Render returns the palette as a discrete floating menu: thick ┃ bars on
// both sides (opencode SplitBorder), grey BackgroundSecondary fill, dynamic
// cmd-column width so descriptions align across all visible items.
func (p *SlashPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	visible := p.Items
	if len(visible) > maxPaletteVisible {
		visible = visible[:maxPaletteVisible]
	}

	w := p.width
	if w < 20 {
		w = 20
	}
	innerW := w - 4

	maxCmd := 0
	for _, it := range visible {
		if cw := lipgloss.Width(it.Cmd); cw > maxCmd {
			maxCmd = cw
		}
	}
	cmdW := maxCmd + 2

	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(innerW)
	cmdStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Text()).Bold(true)
	descStyle := lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted())
	bgPad := lipgloss.NewStyle().Background(bg)

	rows := renderPaletteList(len(visible), p.Cursor, func(i int) string {
		item := visible[i]
		padCmd := fmt.Sprintf("%-*s", cmdW, item.Cmd)
		if i == p.Cursor {
			return selStyle.Render(padCmd + item.Desc)
		}
		raw := cmdStyle.Render(padCmd) + descStyle.Render(item.Desc)
		if visW := lipgloss.Width(raw); visW < innerW {
			raw += bgPad.Render(strings.Repeat(" ", innerW-visW))
		}
		return raw
	})
	return paletteBox(rows, w)
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

// Render — same opencode SplitBorder style as SlashPalette.
func (p *MentionPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	visible := p.Items
	if len(visible) > maxPaletteVisible {
		visible = visible[:maxPaletteVisible]
	}

	w := p.width
	if w < 20 {
		w = 20
	}
	innerW := w - 4

	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(innerW)
	itemStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Text())
	bgPad := lipgloss.NewStyle().Background(bg)

	rows := renderPaletteList(len(visible), p.Cursor, func(i int) string {
		item := visible[i]
		if i == p.Cursor {
			return selStyle.Render(item)
		}
		raw := itemStyle.Render(item)
		if visW := lipgloss.Width(raw); visW < innerW {
			raw += bgPad.Render(strings.Repeat(" ", innerW-visW))
		}
		return raw
	})
	return paletteBox(rows, w)
}

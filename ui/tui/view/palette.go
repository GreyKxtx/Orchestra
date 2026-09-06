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
	{"/attach", "прикрепить файл: /attach <path>"},
	{"/clear", "очистить историю чата"},
	{"/compact", "сжать LLM-контекст сессии"},
	{"/diff", "diff последнего commit"},
	{"/help", "показать команды и клавиши"},
	{"/mcp", "MCP servers: add / edit / test"},
	{"/memory", "показать слои памяти проекта"},
	{"/mode", "текущий режим агента"},
	{"/model", "текущая модель"},
	{"/orchestra", "planner + worker tiers"},
	{"/quit", "выйти из Orchestra TUI"},
	{"/rewind", "checkpoint rewind (скелет)"},
	{"/fork", "ветка от сообщения (оригинал остаётся)"},
	{"/sessions", "сохранённые сессии · /sessions <текст> — поиск по сообщениям"},
	{"/shell", "права на shell: ask ↔ allow"},
	{"/skill", "запустить skill"},
	{"/skills", "список skills"},
	{"/theme", "тема: orchestra ↔ neutral"},
	{"/workflow", "запустить workflow"},
	{"/workflows", "список workflows"},
}

const maxPaletteVisible = 6

// splitBorder — only a left accent bar matching the input box style (▌),
// no right border. Appears as a seamless extension of the input box above it.
var splitBorder = lipgloss.Border{
	Left: "▌",
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

// paletteBox wraps inner content in a left-only accent bar (▌), matching the
// input box style. No right border — the palette appears as a seamless block
// floating above the input.
func paletteBox(inner string, width int, borderColor lipgloss.Color) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	return lipgloss.NewStyle().
		Background(bg).
		Border(splitBorder, false, false, false, true).
		BorderForeground(borderColor).
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
	scroll int // index of the first visible item
	// extraMCP and extraSkills hold commands discovered at runtime — prompts
	// MCP servers offer, and loaded skills, respectively. They are kept apart
	// from AllSlashCmds so neither source can ever bury a built-in: built-ins
	// always list first. They are also kept apart from EACH OTHER: the two
	// sources refresh independently, and one's refresh must not erase the
	// other's commands the way a single shared slot would.
	extraMCP    []SlashCmd
	extraSkills []SlashCmd
}

// SetExtraMCP replaces the MCP-prompt runtime command list. Replacing rather
// than appending matters because the list is refreshed when servers come and
// go, and a stale entry would name a command nothing can run.
func (p *SlashPalette) SetExtraMCP(cmds []SlashCmd) {
	p.extraMCP = append([]SlashCmd(nil), cmds...)
}

// SetExtraSkills replaces the skill-derived runtime command list, on the
// same replace-not-append reasoning as SetExtraMCP.
func (p *SlashPalette) SetExtraSkills(cmds []SlashCmd) {
	p.extraSkills = append([]SlashCmd(nil), cmds...)
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
	all := make([]SlashCmd, 0, len(AllSlashCmds)+len(p.extraMCP)+len(p.extraSkills))
	all = append(all, AllSlashCmds...)
	all = append(all, p.extraMCP...)
	all = append(all, p.extraSkills...)
	filtered := make([]SlashCmd, 0, len(all))
	for _, c := range all {
		if q == "" || strings.Contains(strings.ToLower(c.Cmd[1:]), q) {
			filtered = append(filtered, c)
		}
	}
	p.Items = filtered
	p.Cursor = 0
	p.scroll = 0
}

// CursorUp moves the cursor toward the top, scrolling the window if needed.
func (p *SlashPalette) CursorUp() {
	if p.Cursor > 0 {
		p.Cursor--
		if p.Cursor < p.scroll {
			p.scroll = p.Cursor
		}
	}
}

// CursorDown moves the cursor toward the bottom, scrolling the window if needed.
func (p *SlashPalette) CursorDown() {
	if p.Cursor < len(p.Items)-1 {
		p.Cursor++
		if p.Cursor-p.scroll >= maxPaletteVisible {
			p.scroll = p.Cursor - maxPaletteVisible + 1
		}
	}
}

// Selected returns the Cmd string of the highlighted item, or "" if empty.
func (p *SlashPalette) Selected() string {
	if len(p.Items) == 0 || p.Cursor >= len(p.Items) {
		return ""
	}
	return p.Items[p.Cursor].Cmd
}

// Render returns the palette as a discrete floating menu: ▌ accent bar on
// left only (matching input box style), grey BackgroundSecondary fill,
// dynamic cmd-column width so descriptions align across all visible items.
func (p *SlashPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	// Apply scroll window.
	end := p.scroll + maxPaletteVisible
	if end > len(p.Items) {
		end = len(p.Items)
	}
	visible := p.Items[p.scroll:end]
	cursorInView := p.Cursor - p.scroll

	w := p.width
	if w < 20 {
		w = 20
	}
	// innerW = Width(w) content area: Width excludes borders, Padding(0,1) adds
	// 1 left + 1 right → content = w - 2.
	innerW := w - 2

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

	rows := renderPaletteList(len(visible), cursorInView, func(i int) string {
		item := visible[i]
		padCmd := fmt.Sprintf("%-*s", cmdW, item.Cmd)
		if i == cursorInView {
			return selStyle.Render(padCmd + item.Desc)
		}
		raw := cmdStyle.Render(padCmd) + descStyle.Render(item.Desc)
		if visW := lipgloss.Width(raw); visW < innerW {
			raw += bgPad.Render(strings.Repeat(" ", innerW-visW))
		}
		return raw
	})
	return paletteBox(rows, w, t.Primary())
}

// MentionPalette renders a filtered list of file paths for @-mention completion.
type MentionPalette struct {
	Items  []string
	Cursor int
	width  int
	scroll int
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
	p.Cursor = 0
	p.scroll = 0
}

// CursorUp moves the cursor toward the top, scrolling the window if needed.
func (p *MentionPalette) CursorUp() {
	if p.Cursor > 0 {
		p.Cursor--
		if p.Cursor < p.scroll {
			p.scroll = p.Cursor
		}
	}
}

// CursorDown moves the cursor toward the bottom, scrolling the window if needed.
func (p *MentionPalette) CursorDown() {
	if p.Cursor < len(p.Items)-1 {
		p.Cursor++
		if p.Cursor-p.scroll >= maxPaletteVisible {
			p.scroll = p.Cursor - maxPaletteVisible + 1
		}
	}
}

// Selected returns the currently highlighted file path, or "".
func (p *MentionPalette) Selected() string {
	if len(p.Items) == 0 || p.Cursor >= len(p.Items) {
		return ""
	}
	return p.Items[p.Cursor]
}

// Render — same left-only ▌ accent style as SlashPalette.
func (p *MentionPalette) Render() string {
	if len(p.Items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	end := p.scroll + maxPaletteVisible
	if end > len(p.Items) {
		end = len(p.Items)
	}
	visible := p.Items[p.scroll:end]
	cursorInView := p.Cursor - p.scroll

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
	itemStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Text())
	bgPad := lipgloss.NewStyle().Background(bg)

	rows := renderPaletteList(len(visible), cursorInView, func(i int) string {
		item := visible[i]
		if i == cursorInView {
			return selStyle.Render(item)
		}
		raw := itemStyle.Render(item)
		if visW := lipgloss.Width(raw); visW < innerW {
			raw += bgPad.Render(strings.Repeat(" ", innerW-visW))
		}
		return raw
	})
	return paletteBox(rows, w, t.Primary())
}

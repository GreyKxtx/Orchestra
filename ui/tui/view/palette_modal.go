package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// ModalCommand is a single entry in the command palette modal.
type ModalCommand struct {
	Name string // e.g. "/help"
	Desc string // e.g. "show key bindings"
}

// DefaultModalCommands is the full command list for the Ctrl+K palette.
var DefaultModalCommands = []ModalCommand{
	{"/help", "show key bindings"},
	{"/clear", "clear chat history"},
	{"/model", "show current model"},
	{"/mode", "show current mode"},
	{"/diff", "toggle diff view"},
	{"/apply", "apply pending ops"},
	{"/discard", "discard pending ops"},
	{"/quit", "exit Orchestra"},
}

// PaletteModal is a centered command palette modal (Ctrl+K).
type PaletteModal struct {
	all      []ModalCommand // full command list
	filtered []ModalCommand // after fuzzy filter
	filter   string
	cursor   int
	screenW  int
	screenH  int
	active   bool
}

// NewPaletteModal creates a modal pre-loaded with the default commands.
func NewPaletteModal(screenW, screenH int) *PaletteModal {
	m := &PaletteModal{
		all:      DefaultModalCommands,
		filtered: DefaultModalCommands,
		screenW:  screenW,
		screenH:  screenH,
	}
	return m
}

// SetScreenSize updates the known screen dimensions.
func (m *PaletteModal) SetScreenSize(w, h int) { m.screenW = w; m.screenH = h }

// SetActive shows or hides the modal.
func (m *PaletteModal) SetActive(v bool) {
	m.active = v
	if v {
		m.filter = ""
		m.filtered = m.all
		m.cursor = 0
	}
}

// Active reports whether the modal is visible.
func (m *PaletteModal) Active() bool { return m.active }

// TypeRune appends a rune to the search filter and re-filters.
func (m *PaletteModal) TypeRune(r rune) {
	m.filter += string(r)
	m.applyFilter()
}

// Backspace removes the last rune from the filter.
func (m *PaletteModal) Backspace() {
	if len(m.filter) > 0 {
		m.filter = m.filter[:len(m.filter)-1]
	}
	m.applyFilter()
}

// CursorUp moves selection up.
func (m *PaletteModal) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// CursorDown moves selection down.
func (m *PaletteModal) CursorDown() {
	if m.cursor < len(m.filtered)-1 {
		m.cursor++
	}
}

// Selected returns the name of the currently highlighted command, or "".
func (m *PaletteModal) Selected() string {
	if len(m.filtered) == 0 {
		return ""
	}
	return m.filtered[m.cursor].Name
}

func (m *PaletteModal) applyFilter() {
	m.cursor = 0
	if m.filter == "" {
		m.filtered = m.all
		return
	}
	names := make([]string, len(m.all))
	for i, c := range m.all {
		names[i] = c.Name
	}
	matches := fuzzy.Find(m.filter, names)
	m.filtered = make([]ModalCommand, 0, len(matches))
	for _, match := range matches {
		m.filtered = append(m.filtered, m.all[match.Index])
	}
}

// Render returns the modal string (centered on screen).
func (m *PaletteModal) Render() string {
	if !m.active {
		return ""
	}
	t := theme.CurrentTheme()

	base := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.Text())

	titleStyle := base.Bold(true).Foreground(t.Primary())
	filterStyle := base.Foreground(t.Text())
	selectedStyle := base.
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true)
	normalStyle := base.Foreground(t.Text())
	descStyle := base.Foreground(t.TextMuted())
	hintStyle := base.Foreground(t.TextMuted()).Italic(true)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.TextMuted()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)

	const modalWidth = 44

	var sb strings.Builder
	sb.WriteString(titleStyle.Width(modalWidth).Render("  Commands"))
	sb.WriteString("\n")
	sb.WriteString(filterStyle.Width(modalWidth).Render("  > " + m.filter + "▋"))
	sb.WriteString("\n")
	sb.WriteString(base.Foreground(t.TextMuted()).Width(modalWidth).Render(strings.Repeat("─", modalWidth)))
	sb.WriteString("\n")

	if len(m.filtered) == 0 {
		sb.WriteString(descStyle.Width(modalWidth).Render("  no commands match"))
		sb.WriteString("\n")
	} else {
		for i, cmd := range m.filtered {
			if i == m.cursor {
				line := fmt.Sprintf("  %-10s %s", cmd.Name, cmd.Desc)
				sb.WriteString(selectedStyle.Width(modalWidth).Render(line))
			} else {
				namePart := normalStyle.Render(fmt.Sprintf("  %-10s ", cmd.Name))
				descPart := descStyle.Render(cmd.Desc)
				sb.WriteString(lipgloss.NewStyle().Width(modalWidth).Background(t.BackgroundSecondary()).Render(namePart + descPart))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(base.Foreground(t.TextMuted()).Width(modalWidth).Render(strings.Repeat("─", modalWidth)))
	sb.WriteString("\n")
	sb.WriteString(hintStyle.Width(modalWidth).Render("  ↑↓ navigate · Enter run · Esc close"))

	content := borderStyle.Render(sb.String())
	return lipgloss.Place(m.screenW, m.screenH, lipgloss.Center, lipgloss.Center, content)
}

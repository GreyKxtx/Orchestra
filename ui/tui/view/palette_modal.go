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
	Name     string // e.g. "/help"
	Desc     string // e.g. "show key bindings"
	Category string // e.g. "Agent"; rows are grouped under bold headers in render
}

// DefaultModalCommands is the full command list for the Ctrl+K palette.
// Order here defines render order: items grouped by Category, groups appear
// in declaration order.
var DefaultModalCommands = []ModalCommand{
	// Agent
	{"/provider", "switch LLM provider", "Agent"},
	{"/model", "switch model and tune settings", "Agent"},
	{"/mode", "show current mode", "Agent"},
	// Session
	{"/sessions", "open past sessions", "Session"},
	{"/apply", "apply pending ops", "Session"},
	{"/clear", "clear chat history", "Session"},
	{"/diff", "toggle diff view", "Session"},
	{"/discard", "discard pending ops", "Session"},
	// System
	{"/help", "show key bindings", "System"},
	{"/quit", "exit Orchestra", "System"},
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
	// Preserve fuzzy relevance order — best match first.
	// Category grouping only applies when filter is empty (see branch above).
	m.filtered = make([]ModalCommand, 0, len(matches))
	for _, mt := range matches {
		m.filtered = append(m.filtered, m.all[mt.Index])
	}
}

// Render returns the modal string centered on screen, opencode DialogSelect
// style: borderless block, "Commands" / "esc" header row, plain filter input
// (no ">" prefix or separator lines), full-width primary-bg selected row,
// muted hint row at the bottom.
func (m *PaletteModal) Render() string {
	if !m.active {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	// Width: ≈70% of screen, clamped [56, 90].
	modalW := m.screenW * 70 / 100
	if modalW < 56 {
		modalW = 56
	}
	if modalW > 90 {
		modalW = 90
	}
	if maxW := m.screenW - 4; modalW > maxW {
		modalW = maxW
	}
	if modalW < 30 {
		modalW = 30
	}
	inner := modalW - 8 // outer padding 4 each side

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	mutedStyle := base.Foreground(t.TextMuted())
	primStyle := base.Foreground(t.Primary())
	cmdStyle := base.Foreground(t.Text())
	descStyle := base.Foreground(t.TextMuted())
	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(inner)

	padBg := func(n int) string {
		if n <= 0 {
			return ""
		}
		return base.Render(strings.Repeat(" ", n))
	}
	fitInner := func(s string) string {
		if visW := lipgloss.Width(s); visW < inner {
			return s + padBg(inner-visW)
		}
		return s
	}

	// Header row: "Commands" left, "esc" right.
	title := titleStyle.Render("Commands")
	esc := mutedStyle.Render("esc")
	gap := inner - lipgloss.Width(title) - lipgloss.Width(esc)
	if gap < 1 {
		gap = 1
	}
	header := title + padBg(gap) + esc

	// Filter row: chevron + filter text + cursor (or muted placeholder).
	chev := primStyle.Render("› ")
	var filter string
	if m.filter == "" {
		filter = fitInner(chev + mutedStyle.Render("Search..."))
	} else {
		filter = fitInner(chev + base.Foreground(t.Text()).Render(m.filter) + primStyle.Render("▋"))
	}

	blank := padBg(inner)

	// List rows (or empty-state). Items are grouped by Category: a bold
	// header row precedes each group; groups separated by a blank row.
	var listLines []string
	if len(m.filtered) == 0 {
		listLines = append(listLines, fitInner(mutedStyle.Render("  No results found")))
	} else {
		// Dynamic cmd-column width across filtered items (kept global so
		// descriptions align across groups).
		maxCmd := 0
		for _, c := range m.filtered {
			if cw := lipgloss.Width(c.Name); cw > maxCmd {
				maxCmd = cw
			}
		}
		cmdCol := maxCmd + 2
		const inset = "  "
		accentStyle := base.Foreground(t.Secondary()).Bold(true)

		prevCategory := ""
		for i, c := range m.filtered {
			if c.Category != prevCategory {
				if prevCategory != "" {
					listLines = append(listLines, blank)
				}
				if c.Category != "" {
					listLines = append(listLines, fitInner(accentStyle.Render(c.Category)))
				}
				prevCategory = c.Category
			}
			padCmd := fmt.Sprintf("%-*s", cmdCol, c.Name)
			if i == m.cursor {
				listLines = append(listLines, selStyle.Render(inset+padCmd+c.Desc))
			} else {
				row := cmdStyle.Render(inset+padCmd) + descStyle.Render(c.Desc)
				listLines = append(listLines, fitInner(row))
			}
		}
	}

	// Hint row.
	hint := fitInner(mutedStyle.Render("↑↓ navigate · Enter run · Esc close"))

	sections := []string{blank, header, blank, filter, blank}
	sections = append(sections, listLines...)
	sections = append(sections, blank, hint, blank)
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)

	box := lipgloss.NewStyle().
		Background(bg).
		Padding(0, 4).
		Width(modalW).
		Render(body)

	return lipgloss.Place(m.screenW, m.screenH, lipgloss.Center, lipgloss.Center, box)
}

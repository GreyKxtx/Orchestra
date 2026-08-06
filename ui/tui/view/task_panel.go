package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

const maxExpandedTaskRows = 4

// TodoView is one task row in the task panel.
type TodoView struct {
	ID      string
	Content string
	Status  string // pending | in_progress | done | cancelled
}

// TaskCounts returns done/total for active (non-cancelled) tasks.
func TaskCounts(items []TodoView) (done, total int) {
	for _, it := range items {
		if it.Status == "cancelled" {
			continue
		}
		total++
		if it.Status == "done" {
			done++
		}
	}
	return done, total
}

// TaskPanel renders a compact todo strip above the chat input (not full-screen).
type TaskPanel struct {
	width  int
	open   bool
	items  []TodoView
	scroll int
}

func NewTaskPanel(width int) *TaskPanel {
	return &TaskPanel{width: width}
}

func (p *TaskPanel) SetSize(width int) { p.width = width }
func (p *TaskPanel) SetOpen(v bool)    { p.open = v }
func (p *TaskPanel) Open() bool        { return p.open && len(p.items) > 0 }
func (p *TaskPanel) SetItems(items []TodoView) {
	p.items = append([]TodoView(nil), items...)
	if p.scroll > len(p.items) {
		p.scroll = 0
	}
}

func (p *TaskPanel) Toggle() {
	if len(p.items) == 0 {
		p.open = false
		return
	}
	p.open = !p.open
}

func (p *TaskPanel) ScrollUp() {
	if p.scroll > 0 {
		p.scroll--
	}
}

func (p *TaskPanel) ScrollDown() {
	maxScroll := p.maxScroll()
	if p.scroll < maxScroll {
		p.scroll++
	}
}

func (p *TaskPanel) maxScroll() int {
	n := len(p.items)
	if n <= maxExpandedTaskRows {
		return 0
	}
	return n - maxExpandedTaskRows
}

// VisibleRowsAboveInput returns terminal rows used above the input box.
func (p *TaskPanel) VisibleRowsAboveInput() int {
	if len(p.items) == 0 {
		return 0
	}
	if !p.open {
		return 1
	}
	rows := len(p.items)
	if rows > maxExpandedTaskRows {
		rows = maxExpandedTaskRows
	}
	return 1 + rows // header strip + task rows
}

// RenderAboveInput draws the inline task strip / scrollable list above input.
// Same visual language as the slash palette: ▌ left accent + solid grey fill,
// no top border (avoids black gaps / jagged edges in Windows terminals).
func (p *TaskPanel) RenderAboveInput() string {
	if len(p.items) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	w := p.width
	if w < 20 {
		w = 20
	}
	// Inner content width after ▌ border + horizontal padding(0,1).
	innerW := w - 3
	if innerW < 12 {
		innerW = 12
	}

	done, total := TaskCounts(p.items)
	chevron := "▸"
	hint := "Ctrl+T"
	if p.open {
		chevron = "▾"
		hint = "↑↓ · Ctrl+T"
	}

	title := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).
		Render(fmt.Sprintf("%s Tasks %d/%d", chevron, done, total))
	meta := lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted()).
		Render("  ·  " + hint)
	header := fillBgWidth(title+meta, innerW, bg)

	if !p.open {
		return paletteBox(header, w, t.Primary())
	}

	start := p.scroll
	end := start + maxExpandedTaskRows
	if end > len(p.items) {
		end = len(p.items)
	}
	var rows []string
	rows = append(rows, header)
	for i, it := range p.items[start:end] {
		rows = append(rows, fillBgWidth(renderTodoRow(t, bg, innerW, start+i+1, it), innerW, bg))
	}
	return paletteBox(strings.Join(rows, "\n"), w, t.Primary())
}

// fillBgWidth pads a styled line with background-colored spaces so the grey
// fill spans the full content width (prevents black "holes" on the right).
func fillBgWidth(s string, width int, bg lipgloss.Color) string {
	vis := lipgloss.Width(s)
	if vis >= width {
		return s
	}
	pad := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width-vis))
	return s + pad
}

func renderTodoRow(t theme.Theme, bg lipgloss.Color, maxW int, num int, it TodoView) string {
	statusGlyph, col := todoStatusGlyph(t, it.Status)
	numStyle := lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted())
	textStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Text())
	statusStyle := lipgloss.NewStyle().Background(bg).Foreground(col).Bold(true)

	content := strings.TrimSpace(it.Content)
	if content == "" {
		content = it.ID
	}
	numLabel := fmt.Sprintf("%d.", num)
	// Leave room for "N. " + status glyph on the right.
	rightW := lipgloss.Width(statusGlyph) + 1
	leftW := lipgloss.Width(numLabel) + 1
	budget := maxW - leftW - rightW
	if budget < 4 {
		budget = 4
	}
	if lipgloss.Width(content) > budget {
		runes := []rune(content)
		if len(runes) > budget {
			content = string(runes[:budget-1]) + "…"
		}
	}

	left := numStyle.Render(numLabel+" ") + textStyle.Render(content)
	gap := maxW - lipgloss.Width(left) - lipgloss.Width(statusGlyph)
	if gap < 1 {
		gap = 1
	}
	pad := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap))
	return left + pad + statusStyle.Render(statusGlyph)
}

// todoStatusGlyph is the right-side process indicator (shared Progress* glyphs).
func todoStatusGlyph(t theme.Theme, status string) (glyph string, col lipgloss.Color) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress":
		return ProgressRunning, t.Primary()
	case "done", "completed":
		return ProgressDone, t.Success()
	case "cancelled":
		return ProgressFailed, t.TextMuted()
	default:
		return ProgressPending, t.TextMuted()
	}
}

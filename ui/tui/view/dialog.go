package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Dialog is a self-contained modal screen pushed onto App.dialogStack.
// Update receives every key event while the dialog is on top of the stack.
// Render produces the full visible content (already centered).
type Dialog interface {
	Update(msg tea.Msg) (Dialog, tea.Cmd)
	Render(screenW, screenH int) string
}

// DialogResultMsg is emitted by a dialog when the user makes a choice or
// cancels. App.go inspects Source/Action to chain dialogs and persist state.
type DialogResultMsg struct {
	Source string // "provider" | "model" | "settings"
	Action string // "select" | "cancel" | "save"
	Data   any    // payload (ProviderEntry, ModelEntry, ModelSettings, etc.)
}

// listDialogItem is one row rendered by renderListDialog.
type listDialogItem struct {
	Title       string
	Description string
	Category    string // optional group header
	Disabled    bool   // greyed and skipped on cursor moves
}

// truncRunes truncates s to at most maxR visible runes, appending "…" if cut.
func truncRunes(s string, maxR int) string {
	if maxR <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxR {
		return s
	}
	if maxR == 1 {
		return "…"
	}
	return string(r[:maxR-1]) + "…"
}

// renderListDialog renders a borderless opencode-style modal containing a
// title row ("Title … esc"), filter input, optionally grouped item list, and
// a hint footer. Cursor index addresses items in the order they appear.
func renderListDialog(
	title string,
	items []listDialogItem,
	cursor int,
	filter string,
	placeholder string,
	hint string,
	screenW, screenH int,
) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modalW := screenW * 70 / 100
	if modalW < 56 {
		modalW = 56
	}
	if modalW > 90 {
		modalW = 90
	}
	if maxW := screenW - 4; modalW > maxW {
		modalW = maxW
	}
	if modalW < 30 {
		modalW = 30
	}
	inner := modalW - 8

	base := lipgloss.NewStyle().Background(bg)
	titleStyle := base.Foreground(t.Text()).Bold(true)
	mutedStyle := base.Foreground(t.TextMuted())
	primStyle := base.Foreground(t.Primary())
	cmdStyle := base.Foreground(t.Text())
	descStyle := base.Foreground(t.TextMuted())
	disabledStyle := base.Foreground(t.TextMuted())
	selStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background()).
		Bold(true).
		Width(inner)
	accentStyle := base.Foreground(t.Secondary()).Bold(true)

	padBg := func(n int) string {
		if n <= 0 {
			return ""
		}
		return base.Render(strings.Repeat(" ", n))
	}
	fitInner := func(s string) string {
		w := lipgloss.Width(s)
		if w < inner {
			return s + padBg(inner-w)
		}
		return s
	}

	titleR := titleStyle.Render(title)
	esc := mutedStyle.Render("esc")
	gap := inner - lipgloss.Width(titleR) - lipgloss.Width(esc)
	if gap < 1 {
		gap = 1
	}
	header := titleR + padBg(gap) + esc

	chev := primStyle.Render("› ")
	var filterLine string
	if filter == "" {
		ph := placeholder
		if ph == "" {
			ph = "Search..."
		}
		filterLine = fitInner(chev + mutedStyle.Render(ph))
	} else {
		filterLine = fitInner(chev + base.Foreground(t.Text()).Render(filter) + primStyle.Render("▋"))
	}

	blank := padBg(inner)

	var listLines []string
	if len(items) == 0 {
		listLines = append(listLines, fitInner(mutedStyle.Render("  No results found")))
	} else {
		// Cap title column so long titles don't push descriptions off screen.
		// Title gets at most 40% of inner; description gets the rest minus inset.
		maxTitleW := inner * 40 / 100
		if maxTitleW < 10 {
			maxTitleW = 10
		}
		maxTitle := 0
		for _, it := range items {
			w := lipgloss.Width(it.Title)
			if w > maxTitle {
				maxTitle = w
			}
		}
		if maxTitle > maxTitleW {
			maxTitle = maxTitleW
		}
		titleCol := maxTitle + 2
		// Maximum description width: inner minus inset(2) minus titleCol.
		maxDescW := inner - 2 - titleCol
		if maxDescW < 5 {
			maxDescW = 5
		}

		const inset = "  "
		prevCategory := ""
		for i, it := range items {
			if it.Category != prevCategory {
				if prevCategory != "" {
					listLines = append(listLines, blank)
				}
				if it.Category != "" {
					listLines = append(listLines, fitInner(accentStyle.Render(it.Category)))
				}
				prevCategory = it.Category
			}
			// Truncate title and description to guaranteed-fit widths.
			rowTitle := truncRunes(it.Title, maxTitle)
			rowDesc := truncRunes(it.Description, maxDescW)
			padTitle := fmt.Sprintf("%-*s", titleCol, rowTitle)
			switch {
			case it.Disabled:
				row := disabledStyle.Render(inset+padTitle) + disabledStyle.Render(rowDesc)
				listLines = append(listLines, fitInner(row))
			case i == cursor:
				listLines = append(listLines, selStyle.Render(inset+padTitle+rowDesc))
			default:
				row := cmdStyle.Render(inset+padTitle) + descStyle.Render(rowDesc)
				listLines = append(listLines, fitInner(row))
			}
		}
	}

	// Hard-cap list height so the box never overflows the screen.
	// Fixed overhead: blank+header+blank [+filter+blank] + blank+hint+blank = 8–10 rows.
	const overhead = 10
	maxListRows := screenH - overhead
	if maxListRows < 3 {
		maxListRows = 3
	}
	if len(listLines) > maxListRows {
		listLines = listLines[:maxListRows]
	}

	hintLine := fitInner(mutedStyle.Render(hint))

	sections := []string{blank, header, blank}
	if placeholder != "" || filter != "" {
		sections = append(sections, filterLine, blank)
	}
	sections = append(sections, listLines...)
	sections = append(sections, blank, hintLine, blank)
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)

	box := lipgloss.NewStyle().
		Background(bg).
		Padding(0, 4).
		Width(modalW).
		Render(body)

	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, box)
}

// dialogResultCmd returns a tea.Cmd that emits a DialogResultMsg.
func dialogResultCmd(source, action string, data any) tea.Cmd {
	return func() tea.Msg {
		return DialogResultMsg{Source: source, Action: action, Data: data}
	}
}

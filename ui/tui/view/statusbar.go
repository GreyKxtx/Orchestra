package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// StatusBar renders the bottom status line: spinner · model · ctx% · hints.
type StatusBar struct {
	width      int
	agentBusy  bool
	spinFrame  int // incremented by App on each tick
	model      string
	ctxPercent int    // 0 = unknown, 1–100 = percentage
	errorMsg   string // non-empty → shows error instead of ready
}

// SetWidth updates the bar width.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// SetAgentBusy marks agent as running/idle.
func (s *StatusBar) SetAgentBusy(busy bool) { s.agentBusy = busy }

// AdvanceSpin moves the spinner to the next frame.
func (s *StatusBar) AdvanceSpin() { s.spinFrame = (s.spinFrame + 1) % len(spinnerFrames) }

// SetModel updates the displayed model name.
func (s *StatusBar) SetModel(m string) { s.model = m }

// SetCtxPercent updates context usage (0 = hide).
func (s *StatusBar) SetCtxPercent(pct int) { s.ctxPercent = pct }

// SetError shows an error message on the left side.
func (s *StatusBar) SetError(msg string) { s.errorMsg = msg }

// ClearError clears the error message.
func (s *StatusBar) ClearError() { s.errorMsg = "" }

// Render returns the styled status bar string.
func (s StatusBar) Render() string {
	t := theme.CurrentTheme()

	base := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.Text())

	muted := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.TextMuted())

	// Left: status indicator
	var left string
	switch {
	case s.errorMsg != "":
		errStyle := base.Foreground(t.Error())
		left = errStyle.Render("✗  " + s.errorMsg)
	case s.agentBusy:
		spinStyle := base.Foreground(t.Primary())
		left = spinStyle.Render(spinnerFrames[s.spinFrame] + " Thinking…")
	default:
		okStyle := base.Foreground(t.Success())
		left = okStyle.Render("●  Ready")
	}

	// Right: model · ctx% · hints
	rightParts := []string{}
	if s.model != "" {
		rightParts = append(rightParts, muted.Render(s.model))
	}
	if s.ctxPercent > 0 {
		ctxColor := t.TextMuted()
		if s.ctxPercent > 95 {
			ctxColor = t.Error()
		} else if s.ctxPercent > 80 {
			ctxColor = t.Warning()
		}
		ctxStyle := lipgloss.NewStyle().
			Background(t.BackgroundSecondary()).
			Foreground(ctxColor)
		rightParts = append(rightParts, ctxStyle.Render(fmt.Sprintf("ctx %d%%", s.ctxPercent)))
	}
	rightParts = append(rightParts, muted.Render("ctrl+k cmds"))
	rightParts = append(rightParts, muted.Render("ctrl+o model"))

	right := ""
	for i, p := range rightParts {
		if i > 0 {
			right += muted.Render(" · ")
		}
		right += p
	}

	// Pad to full width
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	pad := s.width - leftWidth - rightWidth - 2
	if pad < 1 {
		pad = 1
	}
	padding := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Render(fmt.Sprintf("%*s", pad, ""))

	return base.Width(s.width).Render(left + padding + right)
}

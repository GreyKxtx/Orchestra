package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// StatusBar renders the bottom status line. Left side shows live agent state
// (running-tool block while busy, or info parts when idle). Right side always
// shows the context-sensitive key hint ("ctrl+k commands" by default).
//
// All parts live on the left so the visual weight stays at the bar's start
// with a 1-cell left padding for breathing room. On narrow terminals the
// rightmost (highest tier) parts are dropped first.
type StatusBar struct {
	width      int
	agentBusy  bool
	spinFrame  int // incremented by App on each tick
	project    string
	model      string
	modelCtx   int    // model's max context length in tokens (0 = unknown)
	tokensUsed int    // tokens used in current session
	tokensMax  int    // session token budget (0 = unknown / use modelCtx)
	ctxPercent int    // 0 = unknown, 1–100 = percentage (legacy, falls back to tokens-based)
	errorMsg   string // non-empty → shows error instead of ready
	hints      string // context-sensitive key hints; shown right-aligned
	// activeTool / activePath describe what the agent is doing right now —
	// rendered as an animated "▌▌ <icon> <name> <path>" block on the left
	// side of the bar while agentBusy is true.
	activeTool string
	activePath string
}

// SetHints sets context-sensitive hint text shown right-aligned on the bar.
func (s *StatusBar) SetHints(h string) { s.hints = h }

// SetActiveTool sets the currently-running tool name + path/preview, rendered
// as an animated left-side block while agentBusy. Empty strings hide it.
func (s *StatusBar) SetActiveTool(name, path string) {
	s.activeTool = name
	s.activePath = path
}

// SetWidth updates the bar width.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// SetAgentBusy marks agent as running/idle.
func (s *StatusBar) SetAgentBusy(busy bool) { s.agentBusy = busy }

// AdvanceSpin moves the spinner to the next frame.
func (s *StatusBar) AdvanceSpin() { s.spinFrame = (s.spinFrame + 1) % len(SpinnerFrames) }

// SetModel updates the displayed model name.
func (s *StatusBar) SetModel(m string) { s.model = m }

// SetProject sets the project name shown on the left of the bar.
func (s *StatusBar) SetProject(p string) { s.project = p }

// SetModelCtx sets the active model's max context length (tokens). 0 hides it.
func (s *StatusBar) SetModelCtx(n int) { s.modelCtx = n }

// SetTokens sets the running token usage and the session budget.
// tokensMax=0 means "use modelCtx" for the X/Y display.
func (s *StatusBar) SetTokens(used, max int) {
	s.tokensUsed = used
	s.tokensMax = max
}

// SetCtxPercent updates context usage (0 = hide).
func (s *StatusBar) SetCtxPercent(pct int) { s.ctxPercent = pct }

// SetError shows an error message on the left side.
func (s *StatusBar) SetError(msg string) { s.errorMsg = msg }

// ClearError clears the error message.
func (s *StatusBar) ClearError() { s.errorMsg = "" }

// Render returns the styled status bar string. Layout:
//
//	[left side: busy block OR info parts]                  [right: key hint]
//
// One blank padding row is prepended so the bar doesn't hug the input box.
func (s StatusBar) Render() string {
	t := theme.CurrentTheme()
	base := lipgloss.NewStyle().Foreground(t.Text())
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	const sidePad = 1
	maxInner := s.width - 2*sidePad
	if maxInner < 1 {
		maxInner = 1
	}

	// Error takes over the whole bar — no point trying to show busy state.
	if s.errorMsg != "" {
		row := base.Foreground(t.Error()).Render("✗  " + s.errorMsg)
		return s.frame(row, sidePad)
	}

	right := muted.Render(s.hints)
	left := s.renderLeft(t, base, muted)

	// Right-align the hint against the inner width budget.
	rightW := lipgloss.Width(right)
	leftW := lipgloss.Width(left)
	gap := maxInner - leftW - rightW
	if gap < 1 {
		gap = 1
		// If overflow, clip the left side.
		budget := maxInner - rightW - 1
		if budget < 1 {
			budget = 1
		}
		left = clipLabel(left, budget)
	}
	row := left + lipgloss.NewStyle().Width(gap).Render("") + right
	return s.frame(row, sidePad)
}

// frame wraps a content row with a leading blank line (top breathing room) and
// applies horizontal side padding.
func (s StatusBar) frame(row string, sidePad int) string {
	padded := lipgloss.NewStyle().Width(s.width).Padding(0, sidePad).Render(row)
	gap := lipgloss.NewStyle().Width(s.width).Render("")
	return gap + "\n" + padded
}

// renderLeft builds the left-side content: animated busy block when the agent
// is working, otherwise the standard project/tokens/ctx info parts.
func (s StatusBar) renderLeft(t theme.Theme, base, muted lipgloss.Style) string {
	if s.agentBusy {
		spin := lipgloss.NewStyle().Foreground(t.Primary()).Render(SpinnerFrames[s.spinFrame%len(SpinnerFrames)])
		// Pulsing accent blocks — three glyphs that cycle in/out via spinFrame
		// for the same OpenCode "moving blocks" feel.
		blocks := s.busyBlocks(t)
		if s.activeTool != "" {
			label := s.activeTool
			if s.activePath != "" {
				label += " " + s.activePath
			}
			return spin + " " + blocks + " " + base.Render(label)
		}
		return spin + " " + blocks + " " + base.Foreground(t.Primary()).Render("Thinking…")
	}

	type tieredPart struct {
		text string
		tier int
	}
	var parts []tieredPart
	if s.project != "" {
		parts = append(parts, tieredPart{base.Bold(true).Render(s.project), 0})
	}
	if s.tokensUsed > 0 {
		max := s.tokensMax
		if max == 0 {
			max = s.modelCtx
		}
		parts = append(parts, tieredPart{muted.Render(formatTokenUsage(s.tokensUsed, max)), 1})
	}
	if s.modelCtx > 0 {
		parts = append(parts, tieredPart{muted.Render(formatCtxLen(int64(s.modelCtx))), 2})
	}
	if s.ctxPercent > 0 {
		ctxColor := t.TextMuted()
		if s.ctxPercent > 95 {
			ctxColor = t.Error()
		} else if s.ctxPercent > 80 {
			ctxColor = t.Warning()
		}
		parts = append(parts, tieredPart{
			lipgloss.NewStyle().Foreground(ctxColor).Render(fmt.Sprintf("ctx %d%%", s.ctxPercent)),
			3,
		})
	}
	hasInfo := len(parts) > 0
	if !hasInfo {
		parts = append(parts, tieredPart{base.Foreground(t.Success()).Render("●  Ready"), 0})
	}

	sep := muted.Render(" · ")
	var b []byte
	for i, p := range parts {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, p.text...)
	}
	return string(b)
}

// busyBlocks renders three accent glyphs that pulse in/out with the spin frame
// for an OpenCode-like "work-in-progress" animation. Frame-modulo picks which
// glyph is bright; the others stay muted, then the highlight moves along.
func (s StatusBar) busyBlocks(t theme.Theme) string {
	const glyph = "▰"
	const dim = "▱"
	prim := lipgloss.NewStyle().Foreground(t.Primary())
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	active := s.spinFrame % 3
	parts := []string{dim, dim, dim}
	parts[active] = glyph
	return prim.Render(parts[active]) + muted.Render(parts[(active+1)%3]) + muted.Render(parts[(active+2)%3])
}

// formatTokenUsage renders "12.3k/128k" or "532/4k" depending on magnitudes.
// max=0 means unknown — only show used.
func formatTokenUsage(used, max int) string {
	if max <= 0 {
		return formatCount(used) + " tokens"
	}
	return formatCount(used) + "/" + formatCount(max)
}


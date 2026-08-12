package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// StatusBar renders the bottom status area.
// Layout: project (left anchor) · metrics · …gap… · hints (right).
// Metrics: busy spinner/tool · tokens% · LSP · profile · cost.
// Tasks live in TaskPanel above the input — not here.
type StatusBar struct {
	width      int
	agentBusy  bool
	spinFrame  int
	project    string
	phase      string // orchestra phase from .orchestra/state.md; "" = no badge
	profile    string // fast | precision | "" (default)
	lspStatus  string // off | idle | installing | active
	lspPercent int    // 0–100 while installing; 0 = omit
	lspHint    string // e.g. gopls
	modelCtx   int
	tokensUsed int
	tokensMax  int
	costUSD    float64
	showCost   bool
	errorMsg   string
	hints      string
	activeTool string
	activePath string
	tokensEst  bool // soft "~" marker when figure is estimate
}

func (s *StatusBar) SetHints(h string) { s.hints = h }
func (s *StatusBar) SetActiveTool(name, path string) {
	s.activeTool = name
	s.activePath = path
}
func (s *StatusBar) SetWidth(w int)            { s.width = w }
func (s *StatusBar) SetAgentBusy(busy bool)    { s.agentBusy = busy }
func (s *StatusBar) AdvanceSpin()              { s.spinFrame = (s.spinFrame + 1) % len(SpinnerFrames) }
func (s *StatusBar) SetModel(_ string)         {} // model shown in input box row
func (s *StatusBar) SetProject(p string)       { s.project = p }
func (s *StatusBar) SetPhase(p string)         { s.phase = p }
func (s *StatusBar) SetProfile(p string)       { s.profile = p }
func (s *StatusBar) SetLSPStatus(st string) { s.lspStatus = st }
func (s *StatusBar) SetLSPProgress(percent int, id string) {
	s.lspPercent = percent
	s.lspHint = id
}
func (s *StatusBar) SetModelCtx(n int) { s.modelCtx = n }
func (s *StatusBar) SetShowCost(v bool)        { s.showCost = v }
func (s *StatusBar) SetTokens(used, max int)   { s.tokensUsed = used; s.tokensMax = max }
func (s *StatusBar) SetTokensEstimated(v bool) { s.tokensEst = v }
func (s *StatusBar) SetCostUSD(v float64)      { s.costUSD = v }
func (s *StatusBar) SetError(msg string)       { s.errorMsg = msg }
func (s *StatusBar) ClearError()               { s.errorMsg = "" }

func (s *StatusBar) Render() string {
	t := theme.CurrentTheme()
	base := lipgloss.NewStyle().Foreground(t.Text())
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	const sidePad = 1
	maxInner := s.width - 2*sidePad
	if maxInner < 1 {
		maxInner = 1
	}

	if s.errorMsg != "" {
		row := base.Foreground(t.Error()).Render("✗  " + s.errorMsg)
		return s.frame(row, sidePad)
	}

	hints := s.hints
	hintsPlain := ""
	hintsRendered := ""
	hintsW := 0
	hintsBudget := 0
	if hints != "" {
		hintsBudget = maxInner / 3
		if hintsBudget < 12 {
			hintsBudget = 12
		}
		if hintsBudget > 40 {
			hintsBudget = 40
		}
		hintsPlain = clipPlain(hints, hintsBudget)
		hintsRendered = muted.Render(hintsPlain)
		hintsW = lipgloss.Width(hintsRendered)
	}

	anchor := s.renderProjectPart(t, base)
	sep := muted.Render(" · ")
	anchorW := lipgloss.Width(anchor)
	sepW := lipgloss.Width(sep)

	metricsBudget := maxInner - anchorW - hintsW
	if anchor != "" {
		metricsBudget -= sepW
	}
	if metricsBudget < 1 {
		metricsBudget = 1
	}

	metrics, _, _ := s.renderMetricsFit(t, base, muted, metricsBudget)
	metricsW := lipgloss.Width(metrics)

	// If metrics still overflow (styled width drift), trim hints further once.
	if hints != "" && anchor != "" && anchorW+sepW+metricsW+hintsW > maxInner {
		overflow := anchorW + sepW + metricsW + hintsW - maxInner
		hintsPlain = clipPlain(hints, hintsBudget-overflow)
		if hintsPlain == "" && len(hints) > 0 {
			hintsPlain = "…"
		}
		hintsRendered = muted.Render(hintsPlain)
		hintsW = lipgloss.Width(hintsRendered)
	}

	var row string
	switch {
	case anchor != "" && metrics != "":
		midGap := maxInner - anchorW - sepW - metricsW - hintsW
		if midGap < 1 {
			midGap = 1
		}
		row = anchor + sep + metrics + lipgloss.NewStyle().Width(midGap).Render("") + hintsRendered
	case anchor != "":
		gap := maxInner - anchorW - hintsW
		if gap < 1 {
			gap = 1
		}
		row = anchor + lipgloss.NewStyle().Width(gap).Render("") + hintsRendered
	case metrics != "":
		gap := maxInner - metricsW - hintsW
		if gap < 1 {
			gap = 1
		}
		row = metrics + lipgloss.NewStyle().Width(gap).Render("") + hintsRendered
	default:
		gap := maxInner - hintsW
		if gap < 1 {
			gap = 1
		}
		row = lipgloss.NewStyle().Width(gap).Render("") + hintsRendered
	}

	return s.frame(row, sidePad)
}

func (s *StatusBar) frame(row string, sidePad int) string {
	padded := lipgloss.NewStyle().Width(s.width).Padding(0, sidePad).Render(row)
	gap := lipgloss.NewStyle().Width(s.width).Render("")
	return gap + "\n" + padded
}

func (s *StatusBar) renderBusyPart(t theme.Theme, base lipgloss.Style) string {
	spin := lipgloss.NewStyle().Foreground(t.Primary()).Render(SpinnerFrames[s.spinFrame%len(SpinnerFrames)])
	blocks := s.busyBlocks(t)
	if s.activeTool != "" {
		label := s.activeTool
		if s.activePath != "" {
			label += " " + clipPlain(s.activePath, 24)
		}
		return spin + " " + blocks + " " + base.Render(clipPlain(label, 32))
	}
	return spin + " " + blocks + " " + base.Foreground(t.Primary()).Render("Думаю…")
}

func (s *StatusBar) renderProjectPart(t theme.Theme, base lipgloss.Style) string {
	name := strings.TrimSpace(s.project)
	if name == "" || name == "." {
		return ""
	}
	return base.Bold(true).Foreground(t.Text()).Render(name)
}

func (s *StatusBar) renderContextPart(t theme.Theme, muted lipgloss.Style) string {
	max := s.tokensMax
	if max <= 0 {
		max = s.modelCtx
	}
	used := s.tokensUsed
	if used <= 0 && max <= 0 {
		return ""
	}

	usage := formatTokenUsage(used, max)
	pct := 0
	if max > 0 && used > 0 {
		pct = used * 100 / max
	}
	body := usage
	if max > 0 {
		body = usage + fmt.Sprintf(" (%d%%)", pct)
	}
	if s.tokensEst {
		body = "~" + body
	}

	col := t.Text()
	if max > 0 && used > 0 {
		switch {
		case pct > 100:
			col = t.Error()
		case pct > 95:
			col = t.Error()
		case pct > 80:
			col = t.Warning()
		}
	}
	_ = muted
	return lipgloss.NewStyle().Foreground(col).Render(body)
}

func (s *StatusBar) renderLSPPart(t theme.Theme) string {
	// off=muted ○ · idle=green ◐ (configured/ready, lazy) · installing=warn · active=green ● (process up)
	switch strings.ToLower(strings.TrimSpace(s.lspStatus)) {
	case "active":
		return lipgloss.NewStyle().Foreground(t.Success()).Render("LSP ●")
	case "installing":
		label := "LSP ◐"
		if s.lspPercent > 0 {
			label = fmt.Sprintf("LSP ◐ %d%%", s.lspPercent)
			if s.lspHint != "" {
				label = fmt.Sprintf("LSP ◐ %s %d%%", s.lspHint, s.lspPercent)
			}
		} else if s.lspHint != "" {
			label = fmt.Sprintf("LSP ◐ %s", s.lspHint)
		}
		return lipgloss.NewStyle().Foreground(t.Warning()).Render(label)
	case "idle":
		return lipgloss.NewStyle().Foreground(t.Success()).Render("LSP ◐")
	default:
		return lipgloss.NewStyle().Foreground(t.TextMuted()).Render("LSP ○")
	}
}

func (s *StatusBar) renderCostPart(t theme.Theme, muted lipgloss.Style) string {
	if s.costUSD <= 0 && !s.showCost {
		return ""
	}
	col := t.TextMuted()
	if s.costUSD >= 0.01 {
		col = t.Warning()
	}
	if s.costUSD >= 0.10 {
		col = t.Error()
	}
	_ = muted
	return lipgloss.NewStyle().Foreground(col).Render(formatCostUSD(s.costUSD))
}

// renderPhasePart shows the Orchestra SDLC phase badge (spec §4.2).
// discovery/documentation/contract — pre-code (warning tint), execution —
// active coding (primary), delivery — post-merge (success), maintenance — muted.
func (s *StatusBar) renderPhasePart(t theme.Theme) string {
	p := strings.ToLower(strings.TrimSpace(s.phase))
	if p == "" {
		return ""
	}
	col := t.Text()
	switch p {
	case "discovery", "documentation", "contract":
		col = t.Warning()
	case "execution":
		col = t.Primary()
	case "delivery":
		col = t.Success()
	case "maintenance":
		col = t.TextMuted()
	}
	return lipgloss.NewStyle().Foreground(col).Render("⦿ " + p)
}

func formatProfileLabel(p string) string {
	p = strings.TrimSpace(strings.ToLower(p))
	switch p {
	case "", "default":
		return ""
	case "fast":
		return "⚡ fast"
	case "precision":
		return "🎯 precision"
	default:
		return p
	}
}

func formatCostUSD(v float64) string {
	switch {
	case v >= 1:
		return fmt.Sprintf("$%.2f", v)
	case v >= 0.01:
		return fmt.Sprintf("$%.3f", v)
	case v > 0:
		return fmt.Sprintf("$%.4f", v)
	default:
		return "$0.00"
	}
}

func (s *StatusBar) busyBlocks(t theme.Theme) string {
	const glyph = "▰"
	const dim = "▱"
	prim := lipgloss.NewStyle().Foreground(t.Primary())
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	active := s.spinFrame % 3
	parts := []string{dim, dim, dim}
	parts[active] = glyph
	return prim.Render(parts[active]) + muted.Render(parts[(active+1)%3]) + muted.Render(parts[(active+2)%3])
}

func formatTokenUsage(used, max int) string {
	if max <= 0 {
		return formatCount(used)
	}
	return formatCount(used) + "/" + formatCount(max)
}

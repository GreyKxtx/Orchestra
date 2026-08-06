package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// fitStatusParts joins styled segments until maxCells is reached. Drops trailing
// segments when necessary; never truncates inside a segment (avoids breaking ANSI).
func fitStatusParts(maxCells int, parts ...string) string {
	if maxCells <= 0 || len(parts) == 0 {
		return ""
	}
	sep := " · "
	sepW := lipgloss.Width(sep)

	var out []string
	used := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		w := lipgloss.Width(p)
		add := w
		if len(out) > 0 {
			add += sepW
		}
		if used+add > maxCells {
			break
		}
		out = append(out, p)
		used += add
	}
	return strings.Join(out, sep)
}

// clipPlain truncates visible plain text to maxCells (no ANSI).
func clipPlain(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxCells {
		return s
	}
	r := []rune(s)
	for len(r) > 1 && lipgloss.Width(string(r)+"…") > maxCells {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// renderMetricsFit builds status metrics within maxCells. Priority: busy · ctx · LSP · cost · profile.
// Tasks live in the strip above the input (TaskPanel), not in the status bar.
func (s *StatusBar) renderMetricsFit(t theme.Theme, base, muted lipgloss.Style, maxCells int) (string, int, int) {
	var segments []string

	if s.agentBusy {
		segments = append(segments, s.renderBusyPart(t, base))
	}
	if ctxPart := s.renderContextPart(t, muted); ctxPart != "" {
		segments = append(segments, ctxPart)
	}
	if lsp := s.renderLSPPart(t); lsp != "" {
		segments = append(segments, lsp)
	}
	if cost := s.renderCostPart(t, muted); cost != "" {
		segments = append(segments, cost)
	}

	prefix := fitStatusParts(maxCells, segments...)

	if prefix == "" && !s.agentBusy {
		ready := base.Foreground(t.Success()).Render("● Ready")
		if lipgloss.Width(ready) <= maxCells {
			return ready, 0, 0
		}
	}

	if prefix != "" {
		remaining := maxCells - lipgloss.Width(prefix)
		if prof := formatProfileLabel(s.profile); prof != "" && remaining > 8 {
			if tail := fitStatusParts(remaining, muted.Render(prof)); tail != "" {
				prefix = prefix + muted.Render(" · ") + tail
			}
		}
	}

	return prefix, 0, 0
}

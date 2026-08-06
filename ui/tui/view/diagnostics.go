package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

func countDiagErrors(diags []state.ToolDiagnostic) int {
	n := 0
	for _, d := range diags {
		if strings.EqualFold(d.Severity, "error") {
			n++
		}
	}
	return n
}

func countDiagWarnings(diags []state.ToolDiagnostic) int {
	n := 0
	for _, d := range diags {
		if strings.EqualFold(d.Severity, "warning") {
			n++
		}
	}
	return n
}

// diagnosticsInlineSuffix appends a compact LSP summary for tool preview lines.
func diagnosticsInlineSuffix(diags []state.ToolDiagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	errs := countDiagErrors(diags)
	if errs > 0 {
		if errs == 1 {
			return " · 1 LSP error"
		}
		return fmt.Sprintf(" · %d LSP errors", errs)
	}
	warns := countDiagWarnings(diags)
	if warns == 1 {
		return " · 1 LSP warning"
	}
	if warns > 0 {
		return fmt.Sprintf(" · %d LSP warnings", warns)
	}
	return fmt.Sprintf(" · %d LSP diags", len(diags))
}

// formatDiagnosticsBlock renders diagnostics for expanded write/edit tool blocks.
func formatDiagnosticsBlock(diags []state.ToolDiagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(t.Warning()).Render("LSP diagnostics:"))
	for _, d := range diags {
		sev := strings.ToLower(strings.TrimSpace(d.Severity))
		col := t.TextMuted()
		switch sev {
		case "error":
			col = t.Error()
		case "warning":
			col = t.Warning()
		}
		line := fmt.Sprintf("  %d:%d %s: %s", d.StartLine, d.StartCol, d.Severity, d.Message)
		if d.Source != "" {
			line += " (" + d.Source + ")"
		}
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(col).Render(line))
	}
	return b.String()
}

package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// RenderSubagentBar draws the compact child-worker tree under the Lead message.
func RenderSubagentBar(tasks []state.SubagentTask, width, spinFrame int) string {
	if len(tasks) == 0 {
		return ""
	}
	th := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(th.TextMuted())
	ok := lipgloss.NewStyle().Foreground(th.Success())
	fail := lipgloss.NewStyle().Foreground(th.Error())
	run := lipgloss.NewStyle().Foreground(th.Primary())
	warn := lipgloss.NewStyle().Foreground(th.Warning())

	if width < 20 {
		width = 20
	}
	inner := width - 2
	if inner < 16 {
		inner = 16
	}

	var b strings.Builder
	for i, task := range tasks {
		prefix := "├── "
		if i == len(tasks)-1 {
			prefix = "└── "
		}
		line := renderSubagentLine(task, i+1, spinFrame, muted, ok, fail, run, warn)
		if lipgloss.Width(prefix+line) > inner {
			line = truncateDisplay(line, inner-lipgloss.Width(prefix))
		}
		b.WriteString(muted.Render(prefix))
		b.WriteString(line)
		if i < len(tasks)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderSubagentLine(task state.SubagentTask, n, spinFrame int, muted, ok, fail, run, warn lipgloss.Style) string {
	label := fmt.Sprintf("[%s #%d]", subagentRoleTitle(task.Role), n)
	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = task.TaskID
	}

	switch task.Status {
	case "done":
		summary := strings.TrimSpace(task.ResultSummary)
		if summary == "" {
			summary = "completed"
		}
		return ok.Render("✅") + " " + ok.Render(label) + " " +
			muted.Render(fmt.Sprintf("Done in %s: %s", formatDuration(task.Duration), summary))
	case "failed":
		summary := strings.TrimSpace(task.ResultSummary)
		if summary == "" {
			summary = "failed"
		}
		return fail.Render("❌") + " " + fail.Render(label) + " " + muted.Render(summary)
	case "queued":
		reason := strings.TrimSpace(task.WaitingReason)
		if reason == "" {
			reason = "waiting target_file lock"
		}
		return warn.Render("⏳") + " " + warn.Render(label) + " " +
			muted.Render(goal) + " " + muted.Render("(queued: "+reason+")")
	default:
		spin := SpinnerFrames[spinFrame%len(SpinnerFrames)]
		detail := goal
		if task.Status == "verifying" && task.Iterations > 0 {
			detail = fmt.Sprintf("%s (LSP verify %d/3)", goal, task.Iterations)
		} else if task.Iterations > 0 {
			detail = fmt.Sprintf("%s (iter %d)", goal, task.Iterations)
		}
		return run.Render(spin) + " " + run.Render("⚙️ "+label) + " " +
			muted.Render(detail+"...") + " " +
			muted.Render(fmt.Sprintf("[%s %s]", task.Status, formatDuration(task.Duration)))
	}
}

func subagentRoleTitle(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "explore":
		return "Explore"
	case "debug":
		return "Debug"
	case "ask":
		return "Ask"
	default:
		return "Worker"
	}
}

func truncateDisplay(s string, max int) string {
	if max <= 1 || lipgloss.Width(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= 1 {
		return s
	}
	for n := len(runes) - 1; n > 0; n-- {
		cut := string(runes[:n]) + "…"
		if lipgloss.Width(cut) <= max {
			return cut
		}
	}
	return "…"
}

package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// queryRequiresCodeChanges reports whether the user turn expects mutating
// tool calls (edit/write) rather than a conversational final answer.
func queryRequiresCodeChanges(query string, todos []tools.TodoItem, mode Mode) bool {
	switch mode {
	case ModeExplore, ModeCompaction, ModeTitle, ModeSummary:
		return false
	}
	for _, t := range todos {
		if t.Status == tools.TodoInProgress {
			return true
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Action / bugfix phrasing in EN + RU.
	markers := []string{
		"fix", "implement", "add ", "create", "update", "change", "refactor",
		"write ", "edit ", "build ", "complete", "finish", "patch",
		"bug", "broken", "doesn't work", "does not work", "not working",
		"исправ", "додел", "добав", "создай", "сделай", "реализ", "почини",
		"напиш", "измени", "обнови", "не работ", "не груз", "нужно ",
	}
	for _, m := range markers {
		if strings.Contains(q, m) {
			return true
		}
	}
	return false
}

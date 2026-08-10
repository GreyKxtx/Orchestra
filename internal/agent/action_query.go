package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// queryRequiresCodeChanges reports whether the user turn expects mutating
// tool calls (edit/write) rather than a conversational final answer.
func queryRequiresCodeChanges(query string, todos []tools.TodoItem, mode Mode) bool {
	switch mode {
	case ModeExplore, ModeAsk, ModePlan, ModeArchitecture, ModeOrchestra,
		ModeCompaction, ModeTitle, ModeSummary:
		// Read-only / planning modes must never be forced into edit/write by
		// action phrasing in the user query ("исправь", "fix", …).
		return false
	}
	for _, t := range todos {
		if t.Status == tools.TodoInProgress || t.Status == tools.TodoPending {
			return true
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Action / bugfix phrasing in EN + RU (avoid past-participle false positives:
	// "что реализовано", "what was implemented").
	markers := []struct {
		needle string
		skip   []string
	}{
		{needle: "fix", skip: []string{"fixed", "fixing", "fixture", "prefix", "suffix"}},
		{needle: "implement", skip: []string{"implemented", "implementation", "implementing"}},
		{needle: "add ", skip: nil},
		{needle: "create", skip: []string{"created", "creates"}},
		{needle: "update", skip: []string{"updated", "updates"}},
		{needle: "change", skip: []string{"changed", "changes"}},
		{needle: "refactor", skip: []string{"refactored"}},
		{needle: "write ", skip: nil},
		{needle: "edit ", skip: []string{"edited", "editing"}},
		{needle: "build ", skip: []string{"building", "built"}},
		{needle: "complete", skip: []string{"completed"}},
		{needle: "finish", skip: []string{"finished"}},
		{needle: "patch", skip: nil},
		{needle: "bug", skip: nil},
		{needle: "broken", skip: nil},
		{needle: "doesn't work", skip: nil},
		{needle: "does not work", skip: nil},
		{needle: "not working", skip: nil},
		{needle: "исправ", skip: []string{"исправлен", "исправлено", "исправлена", "исправлены"}},
		{needle: "додел", skip: nil},
		{needle: "добав", skip: []string{"добавлен", "добавлено", "добавлена", "добавлены"}},
		{needle: "создай", skip: nil},
		{needle: "сделай", skip: []string{"сделан", "сделано", "сделана", "сделаны"}},
		{needle: "реализуй", skip: nil},
		{needle: "реализовать", skip: nil},
		{needle: "почини", skip: nil},
		{needle: "напиш", skip: nil},
		{needle: "измени", skip: []string{"изменен", "изменено", "изменена", "изменены"}},
		{needle: "обнови", skip: nil},
		{needle: "не работ", skip: nil},
		{needle: "не груз", skip: nil},
		{needle: "нужно ", skip: nil},
		{needle: "остав", skip: nil}, // оставь комментарий
		// Do not match bare "коммент"/"comment" — read-only questions like
		// "какой комментарий в файле" must not force edit/write after read.
		{needle: "comment", skip: []string{
			"commented", "comments",
			"what comment", "which comment", "any comment",
			"comment is", "comment at", "comment in", "comment on",
			"какой коммент", "какие коммент", "есть коммент",
		}},
	}
	for _, m := range markers {
		if !strings.Contains(q, m.needle) {
			continue
		}
		skip := false
		for _, s := range m.skip {
			if strings.Contains(q, s) {
				skip = true
				break
			}
		}
		if !skip {
			return true
		}
	}
	return false
}

package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

// queryRequiresCodeChanges reports whether the user turn expects mutating
// tool calls (edit/write) rather than a conversational final answer.
func queryRequiresCodeChanges(query string, todos []tools.TodoItem, mode Mode) bool {
	switch mode {
	case ModeExplore, ModeAsk, ModePlan, ModeArchitecture, ModeOrchestra, ModeVerifier,
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
		{needle: "complete", skip: []string{"completed", "completes", "completing", "completion"}},
		{needle: "finish", skip: []string{"finished", "finishes", "finishing"}},
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
		if !skip && hasUnnegatedOccurrence(q, m.needle) {
			return true
		}
	}
	return false
}

// negations are the words that turn an action into its opposite when they
// directly precede it: "do not change any files", "не исправляй".
var negations = []string{"not ", "n't ", "never ", "no ", "не ", "ни ", "без "}

// hasUnnegatedOccurrence reports whether needle occurs in q at least once
// without a negation right before it. A turn that says "tell me what it
// returned, do not change any files" was being held to editing something.
func hasUnnegatedOccurrence(q, needle string) bool {
	for from := 0; ; {
		i := strings.Index(q[from:], needle)
		if i < 0 {
			return false
		}
		at := from + i
		before := q[:at]
		negated := false
		for _, n := range negations {
			// "n't" ends a word ("don't"); the others must start one, so
			// "knot " or "piano " is not a negation.
			if strings.HasSuffix(before, n) && (n == "n't " || len(before) == len(n) || !isWordByte(before[len(before)-len(n)-1])) {
				negated = true
				break
			}
		}
		if !negated {
			return true
		}
		from = at + len(needle)
	}
}

func isWordByte(c byte) bool {
	return c >= 0x80 || c == '\'' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

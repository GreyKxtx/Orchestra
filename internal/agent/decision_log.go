package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/tools"
)

// logQuestionAnswers records question-tool Q/A pairs into the append-only
// decision log (spec §4.3): answers pass verbatim, no LLM rephrasing. Active
// only for orchestrated sessions (.orchestra/state.md exists) — plain agent
// runs do not accumulate a decision log.
func (a *Agent) logQuestionAnswers(items []tools.QuestionItem, answers []string) {
	root := a.tools.WorkspaceRoot()
	if !decisions.Adopted(root) {
		return
	}
	dept := ""
	if a.opts.IsChild {
		dept = strings.TrimSpace(string(a.opts.Mode))
	}
	entries := make([]decisions.Entry, 0, len(items))
	for i, q := range items {
		ans := ""
		if i < len(answers) {
			ans = answers[i]
		}
		entries = append(entries, decisions.Entry{
			Kind:     "qa",
			Dept:     dept,
			Question: q.Question,
			Answer:   ans,
		})
	}
	if err := decisions.Append(root, entries); err != nil {
		a.logf("decision log append failed: %v", err)
	}
}

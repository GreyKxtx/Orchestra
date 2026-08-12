package tasks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

// Question Barrier (spec §4.3, ADR-2): open_questions[] returned by a
// subagent's task_result are relayed to the user by the Go runtime — verbatim,
// zero L5 tokens. Answers are appended to .orchestra/decisions.md and attached
// to the task_result the parent Lead receives, so the round-trip costs no
// orchestrator turn. When the clarification budget is exhausted
// (max_clarification_rounds, default 2), the runtime stops asking and instructs
// the Lead to proceed on explicit recorded assumptions.

// OpenQuestion is the spec §4.3 open_questions[] element.
type OpenQuestion struct {
	ID       string   `json:"id,omitempty"`
	Dept     string   `json:"dept,omitempty"`
	Text     string   `json:"text"`
	Options  []string `json:"options,omitempty"`
	Blocking bool     `json:"blocking,omitempty"`
}

// DefaultMaxClarificationRounds is the spec §4.3 budget (ADR-5).
const DefaultMaxClarificationRounds = 2

func (r *TaskRunner) resolvedMaxClarificationRounds() int {
	if r.child.MaxClarificationRounds > 0 {
		return r.child.MaxClarificationRounds
	}
	return DefaultMaxClarificationRounds
}

// parseOpenQuestions extracts open_questions[] from a task_result JSON body.
func parseOpenQuestions(raw string) []OpenQuestion {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") || !json.Valid([]byte(raw)) {
		return nil
	}
	var payload struct {
		OpenQuestions []OpenQuestion `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	out := payload.OpenQuestions[:0]
	for _, q := range payload.OpenQuestions {
		if strings.TrimSpace(q.Text) != "" {
			out = append(out, q)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// relayOpenQuestions runs the barrier for one finished task. Returns the
// (possibly augmented) task_result. No-ops when: relay via LLM is explicitly
// requested, no interactive channel exists, the session is not orchestrated,
// or the result carries no questions.
func (r *TaskRunner) relayOpenQuestions(ctx context.Context, taskResult string) string {
	if r.child.RelayViaLLM || r.child.QuestionAsker == nil {
		return taskResult
	}
	qs := parseOpenQuestions(taskResult)
	if len(qs) == 0 {
		return taskResult
	}
	root := r.toolRunner.WorkspaceRoot()
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		return taskResult
	}
	if st.ClarificationRounds >= r.resolvedMaxClarificationRounds() {
		return r.exhaustClarificationBudget(root, taskResult, qs)
	}

	items := make([]tools.QuestionItem, len(qs))
	for i, q := range qs {
		text := q.Text
		if q.Dept != "" {
			text = "[" + q.Dept + "] " + text
		}
		items[i] = tools.QuestionItem{Question: text, Options: q.Options}
	}
	answers, err := r.child.QuestionAsker.Ask(ctx, items)
	if err != nil {
		// The barrier must not turn an answerable result into a failure:
		// the questions stay open in the result for the Lead to handle.
		return taskResult
	}

	st.ClarificationRounds++
	_ = orchestrastate.Save(root, st)

	entries := make([]decisions.Entry, 0, len(qs))
	answerObjs := make([]map[string]string, 0, len(qs))
	for i, q := range qs {
		ans := ""
		if i < len(answers) {
			ans = answers[i]
		}
		entries = append(entries, decisions.Entry{Kind: "qa", Dept: q.Dept, Question: q.Text, Answer: ans})
		answerObjs = append(answerObjs, map[string]string{"id": q.ID, "answer": ans})
	}
	_ = decisions.Append(root, entries)

	return attachBarrierPayload(taskResult, map[string]any{
		"answers":       answerObjs,
		"decisions_ref": decisions.FileRel,
	})
}

// exhaustClarificationBudget records the unanswered questions as forced
// assumptions and tells the Lead to proceed (ADR-5: no infinite Q&A loops).
func (r *TaskRunner) exhaustClarificationBudget(root, taskResult string, qs []OpenQuestion) string {
	entries := make([]decisions.Entry, 0, len(qs))
	for _, q := range qs {
		entries = append(entries, decisions.Entry{
			Kind:     "assumption",
			Dept:     q.Dept,
			Question: q.Text,
			Answer:   "clarification budget exhausted — proceed on a documented assumption",
		})
	}
	_ = decisions.Append(root, entries)
	return attachBarrierPayload(taskResult, map[string]any{
		"clarification_budget_exhausted": true,
		"instruction": "max_clarification_rounds reached: do not re-ask the user. Choose the safest assumption per question, record it in assumptions[], and proceed.",
		"decisions_ref": decisions.FileRel,
	})
}

// attachBarrierPayload merges extra fields into the task_result JSON.
func attachBarrierPayload(taskResult string, extra map[string]any) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(taskResult), &m); err != nil {
		return taskResult
	}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return taskResult
	}
	return string(b)
}

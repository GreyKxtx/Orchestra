package tasks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/playbooks"
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

// relayOpenQuestions runs the barrier for one finished task. It returns the
// (possibly augmented) task_result, and held: the result carries blocking
// questions, so the WorkOrders it returns were written without their answers
// and must not start (holdBatchWorkOrders). No-ops when relay via LLM is
// explicitly requested, the session is not orchestrated, or the result
// carries no questions.
//
// One round is put to the user at a time: children finishing together used
// to ask at once, each in its own prompt. A question already answered in this
// turn — two workers of a department hitting the same gap — is answered from
// that answer and not asked again.
func (r *TaskRunner) relayOpenQuestions(ctx context.Context, taskResult string) (string, bool) {
	if r.child.RelayViaLLM {
		return taskResult, false
	}
	qs := parseOpenQuestions(taskResult)
	if len(qs) == 0 {
		return taskResult, false
	}
	blocking := false
	for _, q := range qs {
		blocking = blocking || q.Blocking
	}
	root := r.toolRunner.WorkspaceRoot()
	if _, found, err := orchestrastate.Load(root); err != nil || !found {
		return taskResult, false
	}
	if r.child.QuestionAsker == nil {
		// Nobody to ask in this run. Saying so is the barrier's job too: a
		// barrier that is silently off reads as a barrier that answered.
		return attachBarrierPayload(taskResult, map[string]any{
			"open_questions_relayed": false,
			"instruction":            "no interactive channel in this run: nobody answered these questions. Ask the user with question, or proceed on assumptions[] you state.",
		}), blocking
	}

	r.barrierMu.Lock()
	defer r.barrierMu.Unlock()
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		return taskResult, false
	}

	answers := make([]string, len(qs))
	var ask []tools.QuestionItem
	var askIdx []int
	for i, q := range qs {
		if ans, ok := r.answered[questionKey(q.Text)]; ok {
			answers[i] = ans
			continue
		}
		text := q.Text
		if q.Dept != "" {
			text = "[" + q.Dept + "] " + text
		}
		ask = append(ask, tools.QuestionItem{Question: text, Options: q.Options})
		askIdx = append(askIdx, i)
	}
	if len(ask) > 0 {
		if st.ClarificationRounds >= r.resolvedMaxClarificationRounds() {
			return r.exhaustClarificationBudget(root, taskResult, qs), false
		}
		got, err := r.child.QuestionAsker.Ask(ctx, ask)
		if err != nil {
			// The barrier must not turn an answerable result into a failure:
			// the questions stay open in the result for the Lead to handle.
			return taskResult, blocking
		}
		// Counted on the state as it is now: the answer took as long as the
		// user took, and a copy loaded before asking would write back a stale
		// phase.
		_, _ = orchestrastate.Update(root, func(st *orchestrastate.State) error {
			st.ClarificationRounds++
			return nil
		})
		if r.answered == nil {
			r.answered = map[string]string{}
		}
		for j, i := range askIdx {
			if j < len(got) {
				answers[i] = got[j]
				r.answered[questionKey(qs[i].Text)] = got[j]
			}
		}
	}

	entries := make([]decisions.Entry, 0, len(qs))
	answerObjs := make([]map[string]string, 0, len(qs))
	for i, q := range qs {
		if slicesContains(askIdx, i) {
			entries = append(entries, decisions.Entry{Kind: "qa", Dept: q.Dept, Question: q.Text, Answer: answers[i]})
		}
		answerObjs = append(answerObjs, map[string]string{"id": q.ID, "answer": answers[i]})
	}
	if len(entries) > 0 {
		_ = decisions.Append(root, entries)
		playbooks.TrySealAllPendingOverlays(root)
	}

	return attachBarrierPayload(taskResult, map[string]any{
		"answers":       answerObjs,
		"decisions_ref": decisions.FileRel,
	}), blocking
}

// holdBatchWorkOrders keeps a Lead's batch from starting: the WorkOrders were
// written before its blocking questions had answers. The Lead revises them
// with the answers — the parent continues it with send_message — and the
// revised batch is relayed then.
func holdBatchWorkOrders(taskResult string) string {
	if _, raws := parseBatchWorkOrders(taskResult); len(raws) == 0 {
		return taskResult
	}
	return attachBarrierPayload(taskResult, map[string]any{
		"batch_workorders_held": true,
		"revise":                "the WorkOrders were written before the blocking questions had answers, so none was started. Continue this Lead with send_message{to: <its dept>} carrying the answers; it returns a revised batch_workorders[] that the runtime relays.",
	})
}

func questionKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func slicesContains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
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
		"instruction":                    "max_clarification_rounds reached: do not re-ask the user. Choose the safest assumption per question, record it in assumptions[], and proceed.",
		"decisions_ref":                  decisions.FileRel,
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

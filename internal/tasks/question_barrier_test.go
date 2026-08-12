package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

type scriptedAsker struct {
	answers []string
	asked   [][]tools.QuestionItem
	err     error
}

func (s *scriptedAsker) Ask(_ context.Context, qs []tools.QuestionItem) ([]string, error) {
	s.asked = append(s.asked, qs)
	if s.err != nil {
		return nil, s.err
	}
	return s.answers, nil
}

func barrierRunner(t *testing.T, root string, cfg ChildAgentConfig) *TaskRunner {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(nil, v, tr, cfg)
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})
	return r
}

func writeBarrierState(t *testing.T, root string, rounds int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\norchestra:\n  phase: execution\n  prd_status: approved\n"
	if rounds > 0 {
		content += "  clarification_rounds: " + string(rune('0'+rounds)) + "\n"
	}
	content += "---\n"
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "state.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseOpenQuestions(t *testing.T) {
	if qs := parseOpenQuestions("plain text result"); qs != nil {
		t.Fatal("non-JSON → no questions")
	}
	if qs := parseOpenQuestions(`{"status":"done"}`); qs != nil {
		t.Fatal("no open_questions field → nil")
	}
	qs := parseOpenQuestions(`{"status":"blocked","open_questions":[{"id":"q1","dept":"backend","text":"retention?","options":["forever","24m"]},{"text":"  "}]}`)
	if len(qs) != 1 || qs[0].ID != "q1" || qs[0].Dept != "backend" {
		t.Fatalf("expected one valid question, got %+v", qs)
	}
}

// The barrier relays open_questions to the user, appends Q/A to decisions.md,
// attaches answers to the result and increments clarification_rounds (§4.3).
func TestRelayOpenQuestions_AsksAndRecords(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 0)
	asker := &scriptedAsker{answers: []string{"24m"}}
	r := barrierRunner(t, root, ChildAgentConfig{QuestionAsker: asker})

	in := `{"status":"blocked","open_questions":[{"id":"q1","dept":"backend","text":"retention?","options":["forever","24m"]}]}`
	out := r.relayOpenQuestions(context.Background(), in)

	if len(asker.asked) != 1 || len(asker.asked[0]) != 1 {
		t.Fatalf("one barrier batch expected, got %+v", asker.asked)
	}
	if !strings.Contains(asker.asked[0][0].Question, "[backend]") {
		t.Fatalf("dept prefix expected: %q", asker.asked[0][0].Question)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("augmented result must stay JSON: %v", err)
	}
	if m["decisions_ref"] != decisions.FileRel {
		t.Fatalf("decisions_ref missing: %v", m)
	}
	answers, _ := m["answers"].([]any)
	if len(answers) != 1 {
		t.Fatalf("attached answers expected: %v", m)
	}

	st, _, err := orchestrastate.Load(root)
	if err != nil || st.ClarificationRounds != 1 {
		t.Fatalf("clarification_rounds must increment, got %+v err=%v", st, err)
	}
	log, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if err != nil || !strings.Contains(string(log), "retention?") || !strings.Contains(string(log), "24m") {
		t.Fatalf("decisions.md must record Q/A verbatim: %v\n%s", err, log)
	}
}

// Beyond max_clarification_rounds the runtime stops asking and instructs the
// Lead to proceed on recorded assumptions (ADR-5).
func TestRelayOpenQuestions_BudgetExhausted(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 2)
	asker := &scriptedAsker{answers: []string{"x"}}
	r := barrierRunner(t, root, ChildAgentConfig{QuestionAsker: asker, MaxClarificationRounds: 2})

	in := `{"status":"blocked","open_questions":[{"id":"q1","text":"tz?"}]}`
	out := r.relayOpenQuestions(context.Background(), in)

	if len(asker.asked) != 0 {
		t.Fatal("budget exhausted → the user must not be asked")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m["clarification_budget_exhausted"] != true {
		t.Fatalf("exhaustion flag expected: %v", m)
	}
	log, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if err != nil || !strings.Contains(string(log), "assumption") {
		t.Fatalf("assumption record expected in decisions.md: %v\n%s", err, log)
	}
}

// Without an orchestrated session or without an asker the barrier is a no-op.
func TestRelayOpenQuestions_NoOpModes(t *testing.T) {
	in := `{"status":"blocked","open_questions":[{"id":"q1","text":"tz?"}]}`

	// No QuestionAsker.
	rootA := t.TempDir()
	writeBarrierState(t, rootA, 0)
	rA := barrierRunner(t, rootA, ChildAgentConfig{})
	if out := rA.relayOpenQuestions(context.Background(), in); out != in {
		t.Fatal("no asker → result must pass through unchanged")
	}

	// No state.md (not an orchestra session).
	rootB := t.TempDir()
	asker := &scriptedAsker{answers: []string{"x"}}
	rB := barrierRunner(t, rootB, ChildAgentConfig{QuestionAsker: asker})
	if out := rB.relayOpenQuestions(context.Background(), in); out != in || len(asker.asked) != 0 {
		t.Fatal("no orchestrated state → barrier off")
	}

	// RelayViaLLM explicitly requested.
	rootC := t.TempDir()
	writeBarrierState(t, rootC, 0)
	rC := barrierRunner(t, rootC, ChildAgentConfig{QuestionAsker: asker, RelayViaLLM: true})
	if out := rC.relayOpenQuestions(context.Background(), in); out != in {
		t.Fatal("relay_via_llm → barrier off")
	}
}

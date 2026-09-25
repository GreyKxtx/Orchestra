package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// orderAsker records, in one log with the workers' model calls, when the user
// was asked.
type orderAsker struct {
	mu  *sync.Mutex
	log *[]string
}

func (a orderAsker) Ask(_ context.Context, qs []tools.QuestionItem) ([]string, error) {
	// The user takes a moment; a worker started before the question would
	// be in the log first.
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(qs))
	for i, q := range qs {
		*a.log = append(*a.log, "ASK "+q.Question)
		out[i] = "answer"
	}
	return out, nil
}

func leadWithQuestion(blocking bool) string {
	out, _ := json.Marshal(map[string]any{
		"summary":          "brief ready",
		"open_questions":   []map[string]any{{"id": "q1", "text": "Which currency?", "blocking": blocking}},
		"batch_workorders": []map[string]any{{"task_id": "w1", "intent": "RELAY-ONE", "target_files": []string{"a.go"}}},
	})
	return string(out)
}

func barrierOrderRunner(t *testing.T, blocking bool) (*TaskRunner, *[]string, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var log []string
	lead := leadWithQuestion(blocking)
	m := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		if strings.Contains(conversation(req), "LEAD-GOAL") {
			return finish(lead)
		}
		mu.Lock()
		log = append(log, "WORKER")
		mu.Unlock()
		return finish(`{"status":"success"}`)
	}}
	r, root := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn(), QuestionAsker: orderAsker{mu: &mu, log: &log}})
	writeBarrierState(t, root, 0)
	return r, &log, &mu
}

// The Lead's workers start only after its questions went to the user. They
// used to be relayed first and run on WorkOrders whose questions were still
// open (ORC-9).
func TestBarrier_RunsBeforeTheRelay(t *testing.T) {
	r, log, mu := barrierOrderRunner(t, false)
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture", Dept: "backend"})
	var out struct {
		Relayed struct {
			TaskIDs []string `json:"task_ids"`
		} `json:"relayed"`
	}
	if err := json.Unmarshal([]byte(res.Result), &out); err != nil || len(out.Relayed.TaskIDs) != 1 {
		t.Fatalf("a non-blocking question does not hold the batch: %v\n%s", err, res.Result)
	}
	if _, err := r.WaitMany(context.Background(), out.Relayed.TaskIDs, 30_000); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*log) < 2 || !strings.HasPrefix((*log)[0], "ASK ") {
		t.Fatalf("the user is asked before any worker runs: %v", *log)
	}
}

// A blocking question means the WorkOrders were written without its answer:
// none starts, and the result says how the Lead revises them.
func TestBarrier_BlockingQuestionHoldsTheBatch(t *testing.T) {
	r, log, mu := barrierOrderRunner(t, true)
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "LEAD-GOAL", SubagentType: "architecture", Dept: "backend"})
	if !strings.Contains(res.Result, `"batch_workorders_held":true`) || !strings.Contains(res.Result, "send_message") {
		t.Fatalf("the batch is held for a revision: %s", res.Result)
	}
	if strings.Contains(res.Result, `"relayed"`) {
		t.Fatalf("no WorkOrder may be relayed: %s", res.Result)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, e := range *log {
		if e == "WORKER" {
			t.Fatalf("a worker ran on a held batch: %v", *log)
		}
	}
}

// Two children returning the same question: the user answers it once. And the
// budget is per phase — a new phase starts with none spent.
func TestBarrier_AsksAQuestionOnceAndBudgetsPerPhase(t *testing.T) {
	root := t.TempDir()
	writeBarrierState(t, root, 0)
	asker := &scriptedAsker{answers: []string{"EUR"}}
	r := barrierRunner(t, root, ChildAgentConfig{QuestionAsker: asker})
	in := `{"status":"blocked","open_questions":[{"id":"q1","text":"Which  currency?"}]}`
	first, _ := r.relayOpenQuestions(context.Background(), in)
	second, _ := r.relayOpenQuestions(context.Background(), strings.Replace(in, "Which  currency?", "which currency?", 1))
	if len(asker.asked) != 1 {
		t.Fatalf("asked %d times, want once", len(asker.asked))
	}
	if !strings.Contains(first, "EUR") || !strings.Contains(second, "EUR") {
		t.Fatalf("both get the answer: %s / %s", first, second)
	}

	st, _, _ := orchestrastate.Load(root)
	if st.ClarificationRounds != 1 {
		t.Fatalf("one round spent: %d", st.ClarificationRounds)
	}
	st.Phase = orchestrastate.PhaseDelivery
	if err := orchestrastate.Save(root, st); err != nil {
		t.Fatal(err)
	}
	if err := orchestrastate.TouchPhaseStamp(root, orchestrastate.PhaseExecution); err != nil {
		t.Fatal(err)
	}
	st, _, _ = orchestrastate.Load(root)
	if st.ClarificationRounds != 0 {
		t.Fatalf("a new phase starts with its own budget: %d", st.ClarificationRounds)
	}
}

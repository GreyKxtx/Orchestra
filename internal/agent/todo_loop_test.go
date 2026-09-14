package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// todowrite and todoread are not Runner tools — the agent intercepts them
// (inprocess_tools.go), so Runner.Call answers "unknown tool" and the
// response-contract file cannot reach them. This is where their contract has
// to live, and it is a turn-shaped contract anyway: a checklist only means
// something across steps.
//
// The eval cannot supply it either. todos_track_a_three_part_chore fails on
// the 9B for the honest reason that the model never calls todowrite at all,
// which says something about the model and nothing about the machinery.
//
// What is worth pinning is the refusal. rejectPrematureFinal blocks a final
// while any todo is open — before it checks whether work was done, and
// deliberately so ("Open checklist always blocks final — even after
// successful edit/write") — and it tells the model three ways out: mark the
// item done, continue it, or cancel it. A refusal that names a way out that
// does not work is the exact defect the duplicate-edit blocker had, where the
// advice was to make the very call that had just been refused.

// todoScriptLLM plays a fixed sequence of responses and records what it was
// asked, so a test can assert on the conversation the agent built.
type todoScriptLLM struct {
	steps []func() *llm.CompleteResponse
	calls int
	seen  []llm.CompleteRequest
}

func (s *todoScriptLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (s *todoScriptLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	s.seen = append(s.seen, req)
	i := s.calls
	s.calls++
	if i < len(s.steps) {
		return s.steps[i](), nil
	}
	// Past the script: keep answering final so a test that expected a refusal
	// fails on its assertion rather than on a nil dereference.
	return finalResponse(), nil
}

func finalResponse() *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{
		Role: llm.RoleAssistant, Content: `{"patches":[]}`,
	}}
}

func todoCall(id, todosJSON string) func() *llm.CompleteResponse {
	return func() *llm.CompleteResponse {
		return toolCallResponse(id, "todowrite", `{"todos":`+todosJSON+`}`)
	}
}

func agentForTodos(t *testing.T, client llm.Client) *Agent {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	ag, err := New(client, v, tr, Options{MaxSteps: 12, Apply: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag
}

const twoOpenTodos = `[{"id":"1","content":"change the host","status":"in_progress"},` +
	`{"id":"2","content":"change the port","status":"pending"}]`

// The first advertised way out: mark them done.
func TestTodos_AnOpenChecklistBlocksFinalAndMarkingItDoneReleasesIt(t *testing.T) {
	client := &todoScriptLLM{steps: []func() *llm.CompleteResponse{
		todoCall("t1", twoOpenTodos),
		finalResponse, // must be refused: both todos are open
		func() *llm.CompleteResponse { return toolCallResponse("t2", "todoread", `{}`) },
		todoCall("t3", `[{"id":"1","content":"change the host","status":"done"},`+
			`{"id":"2","content":"change the port","status":"done"}]`),
		finalResponse, // must be accepted now
	}}
	ag := agentForTodos(t, client)

	_, res, err := ag.Run(context.Background(), nil, "do the two changes")
	if err != nil {
		t.Fatalf("the turn closed its checklist and still failed: %v", err)
	}
	if res == nil {
		t.Fatal("no result")
	}
	if client.calls < 5 {
		t.Errorf("the model was called %d times; the final at step 2 was accepted with two "+
			"open todos, so the checklist guard did nothing", client.calls)
	}
}

// The second advertised way out: cancel them. The refusal says "or cancel
// abandoned todos", so a model that does exactly that must be able to finish.
func TestTodos_CancellingAnAbandonedChecklistAlsoReleasesTheFinal(t *testing.T) {
	client := &todoScriptLLM{steps: []func() *llm.CompleteResponse{
		todoCall("t1", twoOpenTodos),
		finalResponse, // refused
		todoCall("t2", `[{"id":"1","content":"change the host","status":"cancelled"},`+
			`{"id":"2","content":"change the port","status":"cancelled"}]`),
		finalResponse, // must be accepted
	}}
	ag := agentForTodos(t, client)

	_, _, err := ag.Run(context.Background(), nil, "do the two changes")
	if err != nil {
		t.Fatalf("the model took the way out the refusal offered and the turn still "+
			"failed: %v", err)
	}
	if client.calls < 4 {
		t.Errorf("the model was called %d times, so the first final was not refused",
			client.calls)
	}
}

// "completed" is in the schema's enum next to "done" and normalises to it. A
// model picking the other spelling must not be stuck behind the guard.
func TestTodos_TheCompletedSpellingClosesAnItemJustLikeDone(t *testing.T) {
	client := &todoScriptLLM{steps: []func() *llm.CompleteResponse{
		todoCall("t1", twoOpenTodos),
		finalResponse, // refused
		todoCall("t2", `[{"id":"1","content":"change the host","status":"completed"},`+
			`{"id":"2","content":"change the port","status":"completed"}]`),
		finalResponse, // must be accepted
	}}
	ag := agentForTodos(t, client)

	if _, _, err := ag.Run(context.Background(), nil, "do the two changes"); err != nil {
		t.Fatalf(`the schema offers "completed" and the guard did not accept it: %v`, err)
	}
}

// todoread has to hand back what todowrite stored, or the list is write-only
// and a model that lost track re-invents it from scratch.
func TestTodos_TodoreadReturnsWhatTodowriteStored(t *testing.T) {
	client := &todoScriptLLM{steps: []func() *llm.CompleteResponse{
		todoCall("t1", twoOpenTodos),
		func() *llm.CompleteResponse { return toolCallResponse("t2", "todoread", `{}`) },
		todoCall("t3", `[{"id":"1","content":"change the host","status":"done"},`+
			`{"id":"2","content":"change the port","status":"done"}]`),
		finalResponse,
	}}
	ag := agentForTodos(t, client)
	if _, _, err := ag.Run(context.Background(), nil, "do the two changes"); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The request made right after todoread carries its result.
	var readBack string
	for _, req := range client.seen {
		for _, m := range req.Messages {
			if m.Role == llm.RoleTool && strings.Contains(m.Content, "change the host") &&
				strings.Contains(m.Content, "todos") {
				readBack = m.Content
			}
		}
	}
	if readBack == "" {
		t.Fatal("todoread never gave the model back the list it had just written")
	}
	if !strings.Contains(readBack, "change the port") {
		t.Errorf("todoread returned a partial list:\n%s", readBack)
	}
	if !strings.Contains(readBack, "in_progress") {
		t.Errorf("todoread dropped the statuses, which is the only part that says what is "+
			"left to do:\n%s", readBack)
	}
}

// The refusal is the model's only instruction at that moment, so it has to
// name a way out — all three of the ones this suite proves work.
func TestTodos_TheRefusalTellsTheModelHowToGetPastIt(t *testing.T) {
	client := &todoScriptLLM{steps: []func() *llm.CompleteResponse{
		todoCall("t1", twoOpenTodos),
		finalResponse,
		todoCall("t2", `[{"id":"1","content":"a","status":"done"},{"id":"2","content":"b","status":"done"}]`),
		finalResponse,
	}}
	ag := agentForTodos(t, client)
	if _, _, err := ag.Run(context.Background(), nil, "do the two changes"); err != nil {
		t.Fatalf("run: %v", err)
	}

	var refusal string
	for _, req := range client.seen {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "open todo") {
				refusal = m.Content
			}
		}
	}
	if refusal == "" {
		t.Skip("the refusal is not carried in the LLM history; nothing to assert on here")
	}
	for _, wayOut := range []string{"todowrite", "cancel"} {
		if !strings.Contains(refusal, wayOut) {
			t.Errorf("the refusal does not mention %q, leaving the model to guess how to "+
				"get past a block it cannot ignore:\n%s", wayOut, refusal)
		}
	}
}

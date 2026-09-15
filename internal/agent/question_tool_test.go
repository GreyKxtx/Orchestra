package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// question is harness-unreachable: the eval has no one to answer. The one
// existing agent test checks that a question reaches the asker. Nothing checked
// the half the model depends on — what comes BACK: whether the answer arrives,
// in order, and what the model is told when nobody answers.

// questionLLM makes one tool call, then records what it was handed and
// finishes.
type questionLLM struct {
	name, args string
	calls      int
	toolResult string
}

func (q *questionLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (q *questionLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	q.calls++
	if q.calls == 1 {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "q1", Type: "function",
				Function: llm.ToolCallFunc{Name: q.name, Arguments: llm.ToolArguments(q.args)}}},
		}}, nil
	}
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool && m.ToolCallID == "q1" {
			q.toolResult = m.Content
		}
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

// answerEach answers every question it is given, one answer per question.
type answerEach struct {
	got     [][]tools.QuestionItem
	answers []string
	err     error
}

func (a *answerEach) Ask(_ context.Context, qs []tools.QuestionItem) ([]string, error) {
	a.got = append(a.got, qs)
	if a.err != nil {
		return nil, a.err
	}
	out := make([]string, 0, len(qs))
	for i := range qs {
		if i < len(a.answers) {
			out = append(out, a.answers[i])
		}
	}
	return out, nil
}

func runOneToolCall(t *testing.T, mode Mode, asker tools.QuestionAsker, name, args string) *questionLLM {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &questionLLM{name: name, args: args}
	opts := Options{MaxSteps: 4, Mode: mode}
	if asker != nil {
		opts.QuestionAsker = asker
	}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "decide something"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls < 2 {
		t.Fatalf("the turn ended at the tool call; the model never saw the result")
	}
	return client
}

func TestQuestion_TheAnswersReachTheModelInTheOrderAsked(t *testing.T) {
	asker := &answerEach{answers: []string{"yes", "Postgres"}}
	client := runOneToolCall(t, ModePlan, asker, "question",
		`{"questions":[{"question":"Keep the old API?","options":["yes","no"]},{"question":"Which database?"}]}`)

	if len(asker.got) != 1 || len(asker.got[0]) != 2 {
		t.Fatalf("the user was not asked both questions: %+v", asker.got)
	}
	var got struct {
		Answers []string `json:"answers"`
	}
	if err := json.Unmarshal([]byte(client.toolResult), &got); err != nil {
		t.Fatalf("the model was not handed the documented {answers:[...]} shape: %v\n%s", err, client.toolResult)
	}
	if len(got.Answers) != 2 || got.Answers[0] != "yes" || got.Answers[1] != "Postgres" {
		t.Errorf("answers did not come back one per question, in order: %s", client.toolResult)
	}
}

// Nobody answering — the user dismissed the prompt, the session closed — is
// not the same as the user answering nothing. If the model is handed
// {"answers":[]} it proceeds as if the user had no preference.
func TestQuestion_AnUnansweredQuestionIsNotAnEmptyAnswer(t *testing.T) {
	asker := &answerEach{err: errors.New("the user dismissed the question")}
	client := runOneToolCall(t, ModePlan, asker, "question",
		`{"questions":[{"question":"Delete the legacy table?"}]}`)

	if strings.Contains(client.toolResult, `"answers":[]`) || strings.Contains(client.toolResult, `"answers":null`) {
		t.Fatalf("an unanswered question reached the model as an empty answer:\n%s", client.toolResult)
	}
	if !strings.Contains(client.toolResult, "dismissed") {
		t.Errorf("the model is not told why there is no answer:\n%s", client.toolResult)
	}
}

// The schema wraps questions in an array, and a model that flattens it —
// {"question": "..."} — is the ordinary mistake. It must not turn into asking
// the user nothing and telling the model the user said nothing.
func TestQuestion_AFlatQuestionIsNotAskedAsNothing(t *testing.T) {
	asker := &answerEach{answers: []string{"irrelevant"}}
	client := runOneToolCall(t, ModePlan, asker, "question", `{"question":"Which database?"}`)

	for _, qs := range asker.got {
		if len(qs) == 0 {
			t.Errorf("the user was shown a prompt with no question in it")
		}
	}
	if strings.Contains(client.toolResult, `"answers":[]`) || strings.Contains(client.toolResult, `"answers":null`) {
		t.Errorf("a malformed question came back to the model as the user answering nothing:\n%s", client.toolResult)
	}
	if !strings.Contains(client.toolResult, "questions") {
		t.Errorf("the refusal should name the argument the model got wrong:\n%s", client.toolResult)
	}
}

// plan_enter is defined and offered to no mode. The only way to reach its
// handler is a model inventing the name, and the stub must then not pretend a
// switch happened — the build turn goes on in build mode.
func TestPlanEnter_AnInventedCallDoesNotPretendTheModeChanged(t *testing.T) {
	client := runOneToolCall(t, ModeBuild, nil, "plan_enter", `{}`)
	if !strings.Contains(client.toolResult, "not_supported") {
		t.Errorf("the stub's answer changed; a model must be able to tell nothing happened:\n%s", client.toolResult)
	}
	// Tool answers to the model are English; this one was the Russian exception.
	for _, r := range client.toolResult {
		if r >= 0x0400 && r <= 0x04FF {
			t.Errorf("the stub answers the model in Russian:\n%s", client.toolResult)
			break
		}
	}
}

// Pins that plan_enter stays unoffered. Its description reads "Switch to PLAN
// mode", and its handler switches nothing: offering it would be the task_result
// defect again — a tool put in the schema that the runtime then refuses.
func TestPlanEnter_IsOfferedToNoMode(t *testing.T) {
	caps := tools.Capabilities{Exec: true, Web: true, Browser: true}
	for _, mode := range []string{"build", "plan", "explore", "ask", "debug", "architecture",
		"general", "orchestra", "worker", "verifier", "product", "documentation"} {
		for _, sub := range []bool{false, true} {
			for _, d := range tools.ListToolsForMode(mode, caps, sub, true) {
				if d.Function.Name == "plan_enter" {
					t.Errorf("mode %q (subtasks=%v) offers plan_enter, whose handler only answers not_supported", mode, sub)
				}
			}
		}
	}
}

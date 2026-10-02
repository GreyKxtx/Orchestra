package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// Every tool call the UI was told started must be told it ended. A call the
// gates refused and a call the agent answers itself (question, memory_write,
// todowrite...) never were: their rows in the chat spun until the turn ended.
func TestAgent_RefusedAndInProcessToolsReportCompletion(t *testing.T) {
	completions := func(t *testing.T, mode Mode, name, args string) map[string]string {
		t.Helper()
		ec := &eventCollector{}
		llmClient := &inProcessScriptLLM{calls: []struct{ name, args string }{{name, args}}}
		ag, _ := newTestAgent(t, llmClient, Options{Mode: mode, OnEvent: ec.Collect})
		if _, _, err := ag.Run(context.Background(), nil, "go"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		got := map[string]string{}
		for _, ev := range ec.ByKind(llm.StreamEventToolCallCompleted) {
			got[ev.Stream.ToolCallName] = ev.Stream.Content
		}
		return got
	}

	t.Run("in-process", func(t *testing.T) {
		got := completions(t, "", "todowrite", `{"todos":[{"id":"1","content":"draw the disk","status":"done"}]}`)
		if c, ok := got["todowrite"]; !ok {
			t.Error("the todowrite call never reported completion")
		} else if strings.HasPrefix(c, "error:") {
			t.Errorf("todowrite completion = %q, want its result", c)
		}
	})
	t.Run("refused", func(t *testing.T) {
		got := completions(t, ModeAsk, "write", `{"path":"plan.md","content":"x"}`)
		if c, ok := got["write"]; !ok {
			t.Error("the refused write never reported completion")
		} else if !strings.HasPrefix(c, "error: denied") {
			t.Errorf("refused write completion = %q, want the denial", c)
		}
	})
}

// A local model often sends the array as a JSON string —
// {"questions": "[{\"question\": ...}]"}. That failed to unmarshal, the model
// tried again, and the user was asked the same thing over and over.
func TestRunQuestion_AcceptsQuestionsSentAsAString(t *testing.T) {
	asker := &mockQuestionAsker{answers: []string{"js"}}
	ag, _ := newTestAgent(t, &scriptedLLM{}, Options{QuestionAsker: asker})
	in := []byte(`{"questions":"[{\"question\": \"Which engine?\", \"options\": [\"wasm\", \"js\"]}]"}`)
	res := ag.runQuestion(context.Background(), inProcessCall{name: "question", input: in})
	if res.err != nil {
		t.Fatalf("runQuestion: %v", res.err)
	}
	if len(asker.gotQuestions) != 1 || asker.gotQuestions[0].Question != "Which engine?" || len(asker.gotQuestions[0].Options) != 2 {
		t.Fatalf("asked %+v, want the one question with its two options", asker.gotQuestions)
	}
	if !strings.Contains(string(res.out), "js") {
		t.Errorf("out = %s, want the answer", res.out)
	}
}

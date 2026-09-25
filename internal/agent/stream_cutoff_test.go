package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// cutOffLLM streams through the real SSE parser: the first answer's
// connection closes in the middle of a tool call, the second is whole.
type cutOffLLM struct {
	mu    sync.Mutex
	calls int
	saw   [][]llm.Message
}

func (c *cutOffLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (c *cutOffLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	ch, err := c.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	return llm.DrainStreamEvents(ch)
}

func (c *cutOffLLM) CompleteStream(ctx context.Context, req llm.CompleteRequest) (<-chan llm.StreamEvent, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.saw = append(c.saw, append([]llm.Message(nil), req.Messages...))
	c.mu.Unlock()
	body := `data: {"choices":[{"delta":{"content":"Writing the file now"}}]}` + "\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"w1","function":{"name":"write","arguments":"{\"path\":\"a.go\",\"content\":\"package a\\nfunc A() {"}}]}}]}` + "\n"
	if n > 1 {
		body = `data: {"choices":[{"delta":{"content":"{\"type\":\"final\",\"final\":{\"patches\":[]}}"},"finish_reason":"stop"}]}` + "\n" +
			"data: [DONE]\n"
	}
	return llm.ParseSSEStream(ctx, strings.NewReader(body)), nil
}

// LLM-8: a stream whose connection closed mid-answer — no [DONE], no
// finish_reason — was taken as the model's whole answer: the agent ran a
// write whose content was cut off after "func A() {". Now it is an error,
// and the step is retried even though text had already streamed to the
// client, which is told the partial answer was dropped. Only the retried
// answer enters the history.
func TestAgent_ACutOffStreamIsRetriedNotTakenAsTheAnswer(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &cutOffLLM{}
	var notes []string
	ag, err := New(client, v, tr, Options{Mode: ModeBuild, MaxSteps: 4, OnEvent: func(ev AgentEvent) {
		if ev.Stream.Kind == llm.StreamEventRecoverableError {
			notes = append(notes, ev.Stream.Content)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	history, _, err := ag.Run(context.Background(), nil, "write a.go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls < 2 || len(client.saw[1]) != len(client.saw[0]) {
		t.Fatalf("the cut-off step is retried as it was: %d calls", client.calls)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "cut off mid-answer") || !strings.Contains(notes[0], "discarded") {
		t.Fatalf("the client is told the partial answer was dropped: %q", notes)
	}
	for _, m := range append(history, client.saw[len(client.saw)-1]...) {
		if len(m.ToolCalls) > 0 || strings.Contains(m.Content, "Writing the file now") {
			t.Fatalf("the cut-off answer entered the history: %+v", m)
		}
	}
}

// lengthLLM's first answer is cut by the output budget: the provider says so
// with finish_reason "length", and the JSON it sent is half a document.
type lengthLLM struct{ cutOffLLM }

func (c *lengthLLM) CompleteStream(ctx context.Context, req llm.CompleteRequest) (<-chan llm.StreamEvent, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.saw = append(c.saw, append([]llm.Message(nil), req.Messages...))
	c.mu.Unlock()
	body := `data: {"choices":[{"delta":{"content":"{\"type\":\"final\",\"final\":{\"patc"},"finish_reason":"length"}]}` + "\n" + "data: [DONE]\n"
	if n > 1 {
		body = `data: {"choices":[{"delta":{"content":"{\"type\":\"final\",\"final\":{\"patches\":[]}}"},"finish_reason":"stop"}]}` + "\n" + "data: [DONE]\n"
	}
	return llm.ParseSSEStream(ctx, strings.NewReader(body)), nil
}

// The stop reason reaches the agent: an answer cut by max_tokens is not
// taken — the lenient JSON repair made this one a final with no patches —
// and the retry says why, not a bare "fix the JSON" the model answers by
// resending the same long answer.
func TestAgent_AnAnswerCutByMaxTokensIsToldSo(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &lengthLLM{}
	ag, err := New(client, v, tr, Options{Mode: ModeBuild, MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = ag.Run(context.Background(), nil, "write a.go")
	if client.calls < 2 {
		t.Fatalf("the invalid answer is retried: %d calls", client.calls)
	}
	var fed string
	for _, m := range client.saw[1] {
		if m.Role == llm.RoleUser {
			fed += m.Content
		}
	}
	if !strings.Contains(fed, "max_tokens") {
		t.Fatalf("the retry does not say the answer was cut by max_tokens:\n%s", fed)
	}
}

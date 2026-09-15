package agent

import (
	"context"
	"strings"
	"testing"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// requestTextLLM records the text of every request and answers with a final.
type requestTextLLM struct {
	requests []string
}

func (l *requestTextLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *requestTextLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	l.requests = append(l.requests, b.String())
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "Your number is 7342."}}, nil
}

func runSessionTurn(t *testing.T, opts Options, history []llm.Message, query string) *requestTextLLM {
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
	client := &requestTextLLM{}
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 3
	}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), history, query); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) == 0 {
		t.Fatal("the model was never called")
	}
	return client
}

// A session turn opens with the query in history, and the leading user message
// carried it too — every request sent the question twice, and the leading
// message changed each turn, so no prompt cache matched past the system block.
func TestAgent_ASessionTurnSendsTheQueryOnce(t *testing.T) {
	const earlier = "my favourite number is 7342, reply OK"
	const query = "what is my favourite number?"
	history := []llm.Message{
		{Role: llm.RoleUser, Content: promptpkg.UserQueryBlock(earlier)},
		{Role: llm.RoleAssistant, Content: "OK"},
		{Role: llm.RoleUser, Content: promptpkg.UserQueryBlock(query)},
	}
	client := runSessionTurn(t, Options{Mode: ModeAsk}, history, query)
	for i, req := range client.requests {
		if n := strings.Count(req, promptpkg.UserQueryBlock(query)); n != 1 {
			t.Errorf("request %d carries the query %d times, want 1", i+1, n)
		}
		if !strings.Contains(req, earlier) {
			t.Errorf("request %d lost the earlier turn's words", i+1)
		}
	}
}

// Truncation keeps the leading message and drops older history — on a long
// turn, the query with it. Then the leading message must carry it again.
func TestAgent_ATruncatedSessionTurnKeepsTheQuery(t *testing.T) {
	const query = "what is my favourite number?"
	history := []llm.Message{
		{Role: llm.RoleUser, Content: promptpkg.UserQueryBlock(query)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("thinking about numbers. ", 400)},
	}
	client := runSessionTurn(t, Options{Mode: ModeAsk, MaxPromptBytes: 2000}, history, query)
	for i, req := range client.requests {
		if n := strings.Count(req, promptpkg.UserQueryBlock(query)); n != 1 {
			t.Errorf("request %d carries the query %d times, want 1", i+1, n)
		}
	}
}

// A run without a session (agent.run, apply) has no query in history; the
// leading message is still where it lives.
func TestAgent_ARunWithoutHistoryStillSendsTheQuery(t *testing.T) {
	const query = "what is my favourite number?"
	client := runSessionTurn(t, Options{Mode: ModeAsk}, nil, query)
	if n := strings.Count(client.requests[0], promptpkg.UserQueryBlock(query)); n != 1 {
		t.Errorf("the request carries the query %d times, want 1", n)
	}
}

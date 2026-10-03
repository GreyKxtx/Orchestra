package autorouter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

func TestHeuristicClassify(t *testing.T) {
	cases := []struct {
		q    string
		want string
	}{
		{"спланируй архитектуру модуля", "plan"},
		{"plan the auth flow", "plan"},
		{"найди где используется Foo", "explore"},
		{"where is the handler defined", "explore"},
		{"объясни что делает этот код", "ask"},
		{"explain how auth works", "ask"},
		{"добавь функцию Validate", "build"},
		{"fix the nil panic in agent.go", "build"},
	}
	for _, tc := range cases {
		got := HeuristicClassify(tc.q)
		if got.Mode != tc.want {
			t.Errorf("HeuristicClassify(%q)=%q want %q (%s)", tc.q, got.Mode, tc.want, got.Reason)
		}
	}
}

func TestParseDecision(t *testing.T) {
	dec, ok := parseDecision(`{"mode":"plan","confidence":0.9,"reason":"design"}`)
	if !ok || dec.Mode != "plan" {
		t.Fatalf("parseDecision plan: ok=%v dec=%+v", ok, dec)
	}
	_, ok = parseDecision(`{"mode":"orchestra","confidence":1}`)
	if ok {
		t.Fatal("orchestra must be rejected by router")
	}
}

type hangClient struct{}

func (hangClient) Plan(context.Context, string) (string, error) { return "", nil }
func (hangClient) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// LLM-14: the classifier ran with no limit of its own, and a turn waits for
// its mode before it starts. Past ClassifyTimeout the heuristic decides.
func TestClassify_IsBoundedByItsTimeout(t *testing.T) {
	prev := ClassifyTimeout
	ClassifyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { ClassifyTimeout = prev })
	done := make(chan Decision, 1)
	go func() { done <- Classify(context.Background(), hangClient{}, "fix the bug in main.go") }()
	select {
	case d := <-done:
		if want := HeuristicClassify("fix the bug in main.go"); d.Mode != want.Mode {
			t.Fatalf("timed out classifier: mode %q, want the heuristic's %q", d.Mode, want.Mode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Classify was not bounded by ClassifyTimeout")
	}
}

// "задай мне вопрос" asks the assistant to put questions to the user before
// the work — it is not a question about the code. Routed to ask, a build
// conversation went read-only.
func TestHeuristicClassify_AskMeIsNotAsk(t *testing.T) {
	for _, q := range []string{"задай мне вопрос", "Задай мне вопросы перед тем как начать", "спроси меня что нужно", "ask me what you need first"} {
		if got := HeuristicClassify(q); got.Mode == "ask" || got.Mode == "explore" {
			t.Errorf("HeuristicClassify(%q)=%q, want a mode that goes on with the work (%s)", q, got.Mode, got.Reason)
		}
	}
}

type recordingClient struct{ got llm.CompleteRequest }

func (c *recordingClient) Plan(context.Context, string) (string, error) { return "", nil }
func (c *recordingClient) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	c.got = req
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"mode":"build","confidence":0.8,"reason":"continues the build"}`}}, nil
}

// A short reply only means something next to what came before it: the router
// is shown the earlier exchange beside the new message.
func TestClassifyInContext_ShowsTheRouterTheEarlierExchange(t *testing.T) {
	c := &recordingClient{}
	earlier := "user: сделай игру змейка\nassistant: Готово — index.html и game.js."
	d := ClassifyInContext(context.Background(), c, "задай мне вопрос", earlier)
	if d.Mode != "build" {
		t.Fatalf("mode %q, want the client's build", d.Mode)
	}
	var user string
	for _, m := range c.got.Messages {
		if m.Role == llm.RoleUser {
			user = m.Content
		}
	}
	for _, want := range []string{"змейка", "game.js", "задай мне вопрос"} {
		if !strings.Contains(user, want) {
			t.Errorf("router message %q does not carry %q", user, want)
		}
	}
	// Without an earlier exchange the message is the query alone, as before.
	ClassifyInContext(context.Background(), c, "fix main.go", "")
	if got := c.got.Messages[len(c.got.Messages)-1].Content; got != "fix main.go" {
		t.Fatalf("router message %q, want the bare query", got)
	}
}

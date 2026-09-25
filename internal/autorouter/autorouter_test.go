package autorouter

import (
	"context"
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

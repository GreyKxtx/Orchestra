package core

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// toolListLLM records the tool names of the first request and finishes.
type toolListLLM struct {
	mu    sync.Mutex
	names []string
}

func (l *toolListLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *toolListLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	l.mu.Lock()
	if l.names == nil {
		for _, d := range req.Tools {
			l.names = append(l.names, d.Function.Name)
		}
	}
	l.mu.Unlock()
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func (l *toolListLLM) offersBrowser() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.names {
		if strings.HasPrefix(n, "browser.") {
			return true
		}
	}
	return false
}

func newChatCore(t *testing.T, client llm.Client) *Core {
	t.Helper()
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// The chat — agent.run and session.message, what VS Code, the TUI and the web
// UI send — had no way to give a turn the browser: allow_browser existed only
// on skill.invoke and workflow.run. The turn is offered browser tools exactly
// when the client asked for them.
func TestChat_OffersTheBrowserOnlyToTurnsGivenAllowBrowser(t *testing.T) {
	ctx := context.Background()
	for _, allow := range []bool{false, true} {
		client := &toolListLLM{}
		c := newChatCore(t, client)
		if _, err := c.AgentRun(ctx, AgentRunParams{Query: "look at the page", Mode: "build", AllowBrowser: allow}); err != nil {
			t.Fatalf("agent.run allow_browser=%v: %v", allow, err)
		}
		if got := client.offersBrowser(); got != allow {
			t.Errorf("agent.run allow_browser=%v: browser tools offered = %v", allow, got)
		}

		client = &toolListLLM{}
		c = newChatCore(t, client)
		started, err := c.SessionStart(SessionStartParams{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.SessionMessage(ctx, SessionMessageParams{
			SessionID: started.SessionID, Content: "look at the page", Mode: "build", AllowBrowser: allow,
		}); err != nil {
			t.Fatalf("session.message allow_browser=%v: %v", allow, err)
		}
		if got := client.offersBrowser(); got != allow {
			t.Errorf("session.message allow_browser=%v: browser tools offered = %v", allow, got)
		}
	}
}

// Mode agent routes by the query alone, and the router's choices other than
// build — ask, explore, plan — have no browser tools, so a user who switched
// the browser on and asked about a page got a turn that could not open it.
// With the browser on, a route to a mode without it keeps the turn in agent
// mode; without the browser, routing is unchanged.
func TestChat_AgentModeWithTheBrowserDoesNotRouteToAModeWithoutIt(t *testing.T) {
	const query = "explain what the page at http://127.0.0.1:1/ says" // heuristic: ask
	for _, allow := range []bool{false, true} {
		client := &toolListLLM{}
		c := newChatCore(t, client)
		res, err := c.AgentRun(context.Background(), AgentRunParams{Query: query, Mode: "agent", AllowBrowser: allow})
		if err != nil {
			t.Fatalf("allow_browser=%v: %v", allow, err)
		}
		wantMode := "ask"
		if allow {
			wantMode = "agent"
		}
		if res.EffectiveMode != wantMode {
			t.Errorf("allow_browser=%v: effective mode = %q, want %q", allow, res.EffectiveMode, wantMode)
		}
		if got := client.offersBrowser(); got != allow {
			t.Errorf("allow_browser=%v: browser tools offered = %v", allow, got)
		}
	}
}

// The fast profile leaves the browser out even when the turn asks for it —
// and the decision is taken once, so the turn's subagents and skills do not
// keep what the turn itself was denied.
func TestChat_TheFastProfileKeepsTheBrowserOut(t *testing.T) {
	client := &toolListLLM{}
	c := newChatCore(t, client)
	if _, err := c.AgentRun(context.Background(), AgentRunParams{
		Query: "look at the page", Mode: "build", Profile: "fast", AllowBrowser: true,
	}); err != nil {
		t.Fatal(err)
	}
	if client.offersBrowser() {
		t.Error("a fast-profile turn was offered browser tools")
	}
}

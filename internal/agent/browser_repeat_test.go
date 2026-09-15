package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// TestBrowserMCPStub is a minimal MCP server when AGENT_BROWSER_STUB=1: every
// tools/call answers with the tool name and a counter, the way a page answers
// differently after each action.
func TestBrowserMCPStub(t *testing.T) {
	if os.Getenv("AGENT_BROWSER_STUB") != "1" {
		t.Skip("run as a subprocess by the browser tests")
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	n := 0
	for {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "stub"}}})
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			n++
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%s #%d", p.Name, n)}}}})
		}
	}
}

// scriptedToolLLM issues the given tool calls one per step, then finishes, and
// keeps the result each call got.
type scriptedToolLLM struct {
	calls   [][2]string // name, args
	step    int
	results map[string]string
}

func (s *scriptedToolLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *scriptedToolLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			s.results[m.ToolCallID] = m.Content
		}
	}
	if s.step >= len(s.calls) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
			Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	c := s.calls[s.step]
	s.step++
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("c%d", s.step), Type: "function",
			Function: llm.ToolCallFunc{Name: c[0], Arguments: llm.ToolArguments(c[1])}}}}}, nil
}

// A page is not a file: after a click the same browser.snapshot {} returns a
// different page. The duplicate guard blocked the second snapshot on sight —
// "already called with these exact arguments … use edit/write" — and a model
// that needs to see the result of its click asks again until the breaker ends
// the turn. Seen live: a signup skill on qwen3.5-9b through core's skill.invoke
// died on "the model kept calling «browser.snapshot» after it was refused".
func TestAgent_LooksAtThePageAgainAfterActingOnIt(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_BROWSER_STUB", "1")
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{
		AllowBrowser:   true,
		Browser:        config.BrowserConfig{TimeoutMS: 10000},
		BrowserCommand: []string{exe, "-test.run=^TestBrowserMCPStub$"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	client := &scriptedToolLLM{results: map[string]string{}, calls: [][2]string{
		{"browser.navigate", `{"url":"http://127.0.0.1:1/"}`},
		{"browser.snapshot", `{}`},
		{"browser.click", `{"ref":"e9"}`},
		{"browser.snapshot", `{}`},
		{"browser.click", `{"ref":"e9"}`}, // "Next" twice is two pages on
		{"browser.snapshot", `{}`},
	}}
	ag, err := New(client, v, tr, Options{MaxSteps: 12, Mode: ModeBuild, AllowBrowser: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "sign up"); err != nil {
		t.Fatalf("the turn was ended: %v", err)
	}
	for id, want := range map[string]string{"c4": "browser_snapshot", "c5": "browser_click", "c6": "browser_snapshot"} {
		if got := client.results[id]; !strings.Contains(got, want) {
			t.Errorf("call %s was not run against the page: %s", id, got)
		}
	}
}

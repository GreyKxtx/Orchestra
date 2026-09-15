package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// callsLLM sends calls in its first step, records the tool results, and ends.
type callsLLM struct {
	calls   []llm.ToolCall
	steps   int
	results map[string]string
}

func (c *callsLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (c *callsLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	c.steps++
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			c.results[m.ToolCallID] = m.Content
		}
	}
	if c.steps == 1 {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: c.calls}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func toolCall(id, name, args string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(args)}}
}

// runCalls runs one step of calls through an agent in mode, and returns each
// call's tool result by id.
func runCalls(t *testing.T, opts Options, calls ...llm.ToolCall) map[string]string {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("hello from "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &callsLLM{calls: calls, results: map[string]string{}}
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 4
	}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "look"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return client.results
}

// The serial path asked for consent before webfetch only. websearch — which
// sends the query to a search provider — ran for any run that named it.
func TestAgent_RefusesWebsearchWithoutWebConsent(t *testing.T) {
	got := runCalls(t, Options{Mode: ModeBuild}, toolCall("w1", "websearch", `{"query":"orchestra"}`))["w1"]
	if !strings.Contains(got, "denied") || !strings.Contains(got, "consent") {
		t.Errorf("websearch in a run without web consent was not refused:\n%s", got)
	}
}

// webfetch and websearch are ParallelSafe, and product mode lists them
// always, leaving consent to the call. The parallel batch had no consent
// check, so two web calls in one step ran without it.
func TestAgent_RefusesWebToolsInAParallelBatchWithoutConsent(t *testing.T) {
	got := runCalls(t, Options{Mode: ModeProduct},
		// Loopback: refused by the fetcher's SSRF dialer if the gate ever lets it
		// through, so a regression fails here without touching the network.
		toolCall("w1", "webfetch", `{"url":"http://127.0.0.1:1/"}`),
		toolCall("w2", "websearch", `{"query":"orchestra"}`),
	)
	for _, id := range []string{"w1", "w2"} {
		if !strings.Contains(got[id], "denied") || !strings.Contains(got[id], "consent") {
			t.Errorf("%s in a parallel batch without web consent was not refused:\n%s", id, got[id])
		}
	}
}

// permissions.rules were checked on the serial path only. Two reads in one
// step went through the parallel batch, where a deny rule did not exist.
func TestAgent_PermissionRulesHoldInAParallelBatch(t *testing.T) {
	got := runCalls(t, Options{
		Mode:            ModeBuild,
		PermissionRules: []config.PermissionRule{{Tool: "read", Pattern: "a.txt", Action: "deny"}},
	},
		toolCall("r1", "read", `{"path":"a.txt"}`),
		toolCall("r2", "read", `{"path":"b.txt"}`),
	)
	if strings.Contains(got["r1"], "hello from a.txt") || !strings.Contains(got["r1"], "denied") {
		t.Errorf("a read the rules deny ran in a parallel batch:\n%s", got["r1"])
	}
	if !strings.Contains(got["r2"], "hello from b.txt") {
		t.Errorf("the read beside it should still run:\n%s", got["r2"])
	}
}

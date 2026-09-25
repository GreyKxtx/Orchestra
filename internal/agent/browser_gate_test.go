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

// browserToolNames is every browser tool the registry can offer, read from the
// registry rather than listed here.
func browserToolNames(t *testing.T) []string {
	t.Helper()
	without := map[string]bool{}
	for _, d := range tools.ListTools(tools.Capabilities{}) {
		without[d.Function.Name] = true
	}
	var names []string
	for _, d := range tools.ListTools(tools.Capabilities{Browser: true}) {
		if !without[d.Function.Name] {
			names = append(names, d.Function.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("the registry offers no browser tools under allowBrowser")
	}
	return names
}

// validBrowserArgs are arguments each tool accepts, so that without the gate
// every call would get as far as the browser client. A new browser tool without
// an entry fails the test rather than passing it untested.
var validBrowserArgs = map[string]string{
	"browser.navigate":   `{"url":"http://127.0.0.1:1/"}`,
	"browser.snapshot":   `{}`,
	"browser.screenshot": `{}`,
	"browser.click":      `{"ref":"e1"}`,
	"browser.type":       `{"ref":"e1","text":"x"}`,
	"browser.fill":       `{"fields":[{"ref":"e1","value":"x"}]}`,
	"browser.select":     `{"ref":"e1","value":"x"}`,
	"browser.eval":       `{"expression":"1"}`,
	"browser.wait":       `{"text":"x"}`,
	"browser.close":      `{}`,
}

// reachedBrowserClient reports whether a tool result is the browser client
// failing to start its server command — which only a call the agent let through
// can produce. The wording is the OS's: a missing executable is "executable
// file not found" on Windows, which the client turns into its npx hint, and
// "no such file or directory" on Linux, which it reports as is.
func reachedBrowserClient(got string) bool {
	return strings.Contains(got, "Node.js and npx") || strings.Contains(got, "start browser subprocess")
}

// runBrowserCall runs one tool call through an agent whose Runner HAS a browser
// client. The client's command does not exist, so a call that reaches it fails
// with the npx install hint — which tells a call the agent refused apart from
// one it let through.
func runBrowserCall(t *testing.T, allowBrowser bool, name string) string {
	t.Helper()
	args, ok := validBrowserArgs[name]
	if !ok {
		t.Fatalf("no valid arguments recorded for %s", name)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{
		AllowBrowser:   true,
		Browser:        config.BrowserConfig{AllowEval: true},
		BrowserCommand: []string{filepath.Join(root, "no-such-browser-server")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := &questionLLM{name: name, args: args}
	ag, err := New(client, v, tr, Options{MaxSteps: 4, Mode: ModeBuild, AllowBrowser: allowBrowser})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "look at the page"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return client.toolResult
}

// The agent offered browser tools on AllowBrowser and never checked it when a
// call came in: the only gate was the Runner having no browser client. A Runner
// shared by runs that have the browser and runs that do not — core's — had no
// gate at all, so a run never given allow_browser could still drive a browser
// by naming the tool.
func TestAgent_RefusesBrowserToolsARunWasNotGiven(t *testing.T) {
	for _, name := range browserToolNames(t) {
		if got := runBrowserCall(t, true, name); !reachedBrowserClient(got) {
			t.Fatalf("%s does not reach the client even when allowed, so its refusal below would prove nothing:\n%s", name, got)
		}
		got := runBrowserCall(t, false, name)
		if reachedBrowserClient(got) {
			t.Errorf("%s reached the browser in a run without allow_browser:\n%s", name, got)
		}
		if !strings.Contains(got, "allow_browser") {
			t.Errorf("%s: the refusal does not say what would allow it:\n%s", name, got)
		}
	}
}

// The parallel batch takes a call only when its offered definition is marked
// ParallelSafe. Today that happens for browser.snapshot only through mode
// lists, which carry browser tools only with allow_browser — so this test
// builds the definitions a future tool source could hand the agent. Such a
// batch goes to the serial path (batchNeedsSerialGates); the batch's own
// refusal is the backstop behind that.
func TestAgent_RefusesBrowserToolsInAParallelBatchToo(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{
		AllowBrowser:   true,
		BrowserCommand: []string{filepath.Join(root, "no-such-browser-server")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	custom, err := tools.ResolveToolNamesWithPolicy([]string{"read", "browser.snapshot"}, tools.Capabilities{Exec: true, Web: true, Browser: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := range custom {
		custom[i].ParallelSafe = true
	}
	client := &batchLLM{results: map[string]string{}}
	ag, err := New(client, v, tr, Options{MaxSteps: 4, Mode: ModeBuild, CustomTools: custom})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "look"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := client.results["p2"]; reachedBrowserClient(got) || !strings.Contains(got, "allow_browser") {
		t.Errorf("browser.snapshot in a parallel batch was not refused:\n%s", got)
	}
	if got := client.results["p1"]; !strings.Contains(got, "hello") {
		t.Errorf("the read beside it should still run: %s", got)
	}
}

// batchLLM sends [read, browser.snapshot] in one step, then finishes.
type batchLLM struct {
	calls   int
	results map[string]string
}

func (b *batchLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (b *batchLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	b.calls++
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			b.results[m.ToolCallID] = m.Content
		}
	}
	if b.calls == 1 {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "p1", Type: "function", Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"a.txt"}`)}},
			{ID: "p2", Type: "function", Function: llm.ToolCallFunc{Name: "browser.snapshot", Arguments: llm.ToolArguments(`{}`)}},
		}}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func TestAgent_LetsBrowserToolsThroughWhenTheRunHasThem(t *testing.T) {
	got := runBrowserCall(t, true, "browser.navigate")
	if !reachedBrowserClient(got) {
		t.Errorf("a run with allow_browser did not reach the browser client:\n%s", got)
	}
}

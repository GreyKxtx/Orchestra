package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
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

// reachedBrowserClient is what the browser client answers when its server
// command cannot be started.
const reachedBrowserClient = "Node.js and npx"

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
		if got := runBrowserCall(t, true, name); !strings.Contains(got, reachedBrowserClient) {
			t.Fatalf("%s does not reach the client even when allowed, so its refusal below would prove nothing:\n%s", name, got)
		}
		got := runBrowserCall(t, false, name)
		if strings.Contains(got, reachedBrowserClient) {
			t.Errorf("%s reached the browser in a run without allow_browser:\n%s", name, got)
		}
		if !strings.Contains(got, "allow_browser") {
			t.Errorf("%s: the refusal does not say what would allow it:\n%s", name, got)
		}
	}
}

func TestAgent_LetsBrowserToolsThroughWhenTheRunHasThem(t *testing.T) {
	got := runBrowserCall(t, true, "browser.navigate")
	if !strings.Contains(got, reachedBrowserClient) {
		t.Errorf("a run with allow_browser did not reach the browser client:\n%s", got)
	}
}

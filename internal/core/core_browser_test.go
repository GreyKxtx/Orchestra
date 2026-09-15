package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// skill.invoke and workflow.run take allow_browser and offer browser tools to
// the model on it, but core's Runner had no browser client, so every one of
// those calls answered "browser tools require --allow-browser". The Runner now
// carries the client — started only on first use — and whether a run may use
// it is decided per run, by the agent.
func TestCore_RunnerCanServeBrowserToolsToRunsThatAllowThem(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.tools.BrowserEnabled() {
		t.Error("core's Runner has no browser client, so allow_browser runs are offered tools that cannot run")
	}
}

// tool.call has no allow_browser and no run to ask; with the client now on the
// shared Runner it would otherwise open a browser for any connected client.
func TestCore_ToolCallRefusesBrowserTools(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.ToolCall(context.Background(), ToolCallParams{
		Name: "browser.navigate", Input: json.RawMessage(`{"url":"http://127.0.0.1:1/"}`),
	})
	if err == nil {
		t.Fatal("tool.call drove the browser with no run and no consent")
	}
	if !strings.Contains(err.Error(), "allow_browser") {
		t.Errorf("the refusal does not say where browser tools are available: %v", err)
	}
}

// tool.call held bash to exec.confirm and let webfetch and websearch through
// with no check, so any connected client could reach the network where the
// config had not consented to it.
func TestCore_ToolCallHoldsWebToolsToWebConfirm(t *testing.T) {
	for _, consent := range []bool{false, true} {
		root := t.TempDir()
		cfg := config.DefaultConfig(root)
		confirm := !consent
		cfg.Web.Confirm = &confirm
		if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
			t.Fatal(err)
		}
		c, err := New(root, Options{ToolsOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		for name, input := range map[string]string{
			// Loopback: the fetcher's SSRF dialer refuses it, so an allowed call
			// fails without touching the network.
			"webfetch":  `{"url":"http://127.0.0.1:1/"}`,
			"websearch": `{"query":"orchestra"}`,
		} {
			_, err := c.ToolCall(context.Background(), ToolCallParams{Name: name, Input: json.RawMessage(input)})
			refused := err != nil && strings.Contains(err.Error(), "consent")
			if refused == consent {
				t.Errorf("web.confirm=%v: %s refused for consent = %v (err: %v)", confirm, name, refused, err)
			}
		}
		_ = c.Close()
	}
}

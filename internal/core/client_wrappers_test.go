package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// llm.fallback_provider and llm.router are wrappers around the provider
// client, and every site that built a client applied them by hand. The
// hand-assembly drifted both ways.
//
// The core applied the standby when it constructed the client, when it
// discovered model limits and when it resolved a client by name — and forgot
// it in the fourth place, the config refresh. So the standby worked until the
// user saved any setting, and then quietly did nothing for the rest of the
// process, which is the shape of outage that looks like the model is down.
//
// The router was worse: it was applied only under `apply` and `workflow`, so
// on every core-backed surface (TUI, VS Code, web, desktop) the key passed
// config validation and was ignored.
//
// Both now go through llm.BuildClient. These tests pin the wrappers where a
// user would notice them missing.

// wrappedConfig is a project whose llm block asks for both wrappers, each
// pointing at an endpoint distinct from the main one (a standby that shares
// the primary's endpoint is deliberately skipped — it goes down with it).
func wrappedConfig(t *testing.T, root string) *config.ProjectConfig {
	t.Helper()
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "main-model"
	cfg.LLM.FallbackProvider = "standby"
	cfg.LLM.Router = llm.RouterConfig{Enabled: true, FastProvider: "quick", ThresholdBytes: 2048}
	cfg.Providers = map[string]llm.LLMConfig{
		"standby": {APIBase: "http://127.0.0.1:10/v1", APIKey: "k", Model: "standby-model"},
		"quick":   {APIBase: "http://127.0.0.1:11/v1", APIKey: "k", Model: "quick-model"},
	}
	return cfg
}

// unwrapTo walks the wrapper chain looking for a client of the wanted shape.
func hasFallback(c llm.Client) bool {
	for i := 0; i < 8 && c != nil; i++ {
		if _, ok := c.(*llm.FallbackClient); ok {
			return true
		}
		u, ok := c.(interface{ Unwrap() llm.Client })
		if !ok {
			return false
		}
		c = u.Unwrap()
	}
	return false
}

func TestCoreBuildsAClientCarryingBothWrappers(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	if err := config.Save(cfgPath, wrappedConfig(t, root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if _, ok := c.llmClient.(*llm.RouterClient); !ok {
		t.Errorf("llm.router is configured and enabled, yet the core's client is %T; "+
			"the key passes validation and does nothing on every core-backed surface", c.llmClient)
	}
	if !hasFallback(c.llmClient) {
		t.Errorf("llm.fallback_provider is configured, yet no standby in the chain (%T)", c.llmClient)
	}
}

// The regression: saving any setting rebuilt the client bare.
func TestConfigRefreshKeepsTheWrappers(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	if err := config.Save(cfgPath, wrappedConfig(t, root)); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// An external edit to the llm block — the shape of every model switch and
	// every settings save.
	ext, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ext.LLM.Model = "switched-model"
	if err := config.Save(cfgPath, ext); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}

	c.RefreshConfigIfChanged()

	if c.cfg.LLM.Model != "switched-model" {
		t.Fatalf("refresh did not happen: model = %q", c.cfg.LLM.Model)
	}
	if !hasFallback(c.llmClient) {
		t.Errorf("the standby was dropped by the rebuild (%T): saving a setting "+
			"used to switch llm.fallback_provider off for the rest of the process", c.llmClient)
	}
	if _, ok := c.llmClient.(*llm.RouterClient); !ok {
		t.Errorf("the router was dropped by the rebuild (%T)", c.llmClient)
	}
}

// An injected client is the test seam and must stay exactly what was injected.
func TestConfigRefreshLeavesAnInjectedClientAlone(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	if err := config.Save(cfgPath, wrappedConfig(t, root)); err != nil {
		t.Fatal(err)
	}
	stub := refreshStubLLM{}
	c, err := New(root, Options{LLMClient: stub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ext, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ext.LLM.Model = "switched-model"
	if err := config.Save(cfgPath, ext); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}

	c.RefreshConfigIfChanged()

	if _, ok := c.llmClient.(refreshStubLLM); !ok {
		t.Fatalf("an injected client was replaced by the refresh: %T", c.llmClient)
	}
}

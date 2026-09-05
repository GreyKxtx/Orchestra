package core

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// TestNew_ToolsOnly_SkipsLLMClientAndMCPManager verifies the fast, LLM-free
// startup path a tool-only MCP server needs: no LLM client construction (and
// therefore no network call to resolve model limits), no configured MCP
// client servers started, but the tools.Runner is still built normally.
func TestNew_ToolsOnly_SkipsLLMClientAndMCPManager(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")

	cfg := config.DefaultConfig(root)
	// Deliberately unreachable: if ToolsOnly didn't skip LLM client
	// construction, ResolveModelLimits' network dial would make this test
	// slow (and flaky under -race) instead of just wrong.
	cfg.LLM.APIBase = "http://127.0.0.1:1/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "unused-model"
	cfg.MCP.Servers = []config.MCPServerConfig{{
		Name:    "should-not-start",
		Command: []string{"does-not-exist-on-this-machine"},
	}}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{ToolsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if c.llmClient != nil {
		t.Error("ToolsOnly must not construct an LLM client")
	}
	if c.mcpManager != nil {
		t.Error("ToolsOnly must not start configured MCP client servers")
	}
	if c.tools == nil {
		t.Error("ToolsOnly must still construct the tools.Runner")
	}
}

// TestNew_WithoutToolsOnly_StillConstructsLLMClient guards against a future
// edit accidentally making ToolsOnly the default behavior.
func TestNew_WithoutToolsOnly_StillConstructsLLMClient(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:1/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "unused-model"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if c.llmClient == nil {
		t.Error("without ToolsOnly, New must still construct an LLM client as before")
	}
}

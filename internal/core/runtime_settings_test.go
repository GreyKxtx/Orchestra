package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/prompt"
)

func TestAgentsUpsertDelete(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	cfg.LLM.APIKey = "k"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	persist := true
	res, err := c.AgentsUpsert(core.AgentsUpsertParams{
		Agent: config.AgentDefinition{
			Name:         "reviewer",
			SystemPrompt: "review carefully",
			Tools:        []string{"read", "grep"},
		},
		Persist: &persist,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(res.Agents) != 1 || res.Agents[0].Name != "reviewer" || !res.Persisted {
		t.Fatalf("unexpected: %+v", res)
	}

	_, err = c.AgentsUpsert(core.AgentsUpsertParams{
		Agent: config.AgentDefinition{Name: "build", SystemPrompt: "x"},
	})
	if err == nil {
		t.Fatal("expected built-in name rejection")
	}

	del, err := c.AgentsDelete(core.AgentsDeleteParams{Name: "reviewer", Persist: &persist})
	if err != nil {
		t.Fatal(err)
	}
	if len(del.Agents) != 0 {
		t.Fatalf("expected empty agents, got %+v", del.Agents)
	}
}

func TestSystemPromptOverride(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	cfg.LLM.APIKey = "k"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	content := "You are a custom system prompt."
	set, err := c.RuntimeSetSystemPrompt(core.RuntimeSetSystemPromptParams{Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if !set.HasOverride {
		t.Fatal("expected override")
	}
	got := prompt.LoadSystemOverride(root)
	if got != content {
		t.Fatalf("got %q", got)
	}
	g, err := c.RuntimeGetSystemPrompt(core.RuntimeGetSystemPromptParams{})
	if err != nil || !g.HasOverride || g.Content != content {
		t.Fatalf("get: %+v err=%v", g, err)
	}
	cleared, err := c.RuntimeSetSystemPrompt(core.RuntimeSetSystemPromptParams{Clear: true})
	if err != nil || cleared.HasOverride {
		t.Fatalf("clear: %+v err=%v", cleared, err)
	}
	if _, err := os.Stat(prompt.SystemOverridePath(root)); !os.IsNotExist(err) {
		t.Fatalf("file should be gone: %v", err)
	}
}

func TestMCPUpsertDeleteDisabled(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	cfg.LLM.APIKey = "k"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	persist := true
	// Disabled server with fake command — ReplaceMCP should not hang.
	up, err := c.MCPUpsert(context.Background(), core.MCPUpsertParams{
		Server: core.MCPServerParams{
			Name:     "fake",
			Command:  []string{"orchestra-mcp-does-not-exist-xyz"},
			Disabled: true,
		},
		Persist: &persist,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(up.Servers) != 1 || up.Servers[0].Status != "disabled" {
		t.Fatalf("unexpected: %+v", up)
	}
	loaded, err := config.Load(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.MCP.Servers) != 1 || loaded.MCP.Servers[0].Name != "fake" {
		t.Fatalf("disk: %+v", loaded.MCP.Servers)
	}

	_, err = c.MCPSetDisabled(context.Background(), core.MCPSetDisabledParams{
		Name: "fake", Disabled: false, Persist: &persist,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := c.MCPList(core.MCPListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Servers) != 1 {
		t.Fatalf("list: %+v", list)
	}
	// Likely error status (binary missing) — not disabled.
	if list.Servers[0].Status == "disabled" {
		t.Fatalf("expected enabled attempt, got %+v", list.Servers[0])
	}

	del, err := c.MCPDelete(context.Background(), core.MCPDeleteParams{Name: "fake", Persist: &persist})
	if err != nil {
		t.Fatal(err)
	}
	if len(del.Servers) != 0 {
		t.Fatalf("expected empty: %+v", del)
	}
}

func TestIndexStatusConfigure(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	cfg.LLM.APIKey = "k"
	cfg.ExcludeDirs = []string{".git", "vendor"}
	cfg.Embed.Model = "embed-model"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	st, err := c.IndexStatus(core.IndexStatusParams{})
	if err != nil {
		t.Fatal(err)
	}
	if st.ProjectRoot != root {
		t.Fatalf("project root: got %q", st.ProjectRoot)
	}
	if !st.Graph.Available {
		t.Fatal("expected graph available")
	}

	ctxKB := 128
	persist := true
	cfgRes, err := c.IndexConfigure(core.IndexConfigureParams{
		ContextLimitKB: &ctxKB,
		EmbedModel:     "nomic-embed",
		Persist:        &persist,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfgRes.ContextLimitKB != 128 || cfgRes.Embed.Model != "nomic-embed" || !cfgRes.Persisted {
		t.Fatalf("unexpected configure: %+v", cfgRes)
	}
	loaded, err := config.Load(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Embed.Model != "nomic-embed" || loaded.ContextLimit != 128 {
		t.Fatalf("yaml not updated: embed=%q ctx=%d", loaded.Embed.Model, loaded.ContextLimit)
	}
}

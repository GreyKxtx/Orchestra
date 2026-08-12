package core_test

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
)

func TestRuntimeGetConfigureOrchestra(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.Provider = "openrouter"
	cfg.LLM.Model = "anthropic/claude-3.5-sonnet"
	cfg.LLM.APIBase = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-test"
	cfg.Providers = map[string]config.LLMConfig{
		"openrouter": {Provider: "openrouter", APIBase: "https://openrouter.ai/api/v1", APIKey: "sk-test"},
	}
	cfg.Orchestra.Planner.Provider = "openrouter"
	cfg.Orchestra.Planner.Model = "anthropic/claude-3.5-sonnet"
	cfg.Orchestra.Tiers = []config.OrchestraTier{
		{Name: "complex", Provider: "openrouter", Model: "openai/gpt-4o", Models: []string{"openai/gpt-4o", "google/gemini-pro"}},
		{Name: "focused", Provider: "openrouter", Model: "anthropic/claude-3-haiku"},
		{Name: "micro", Provider: "openrouter", Model: "qwen/qwen-2.5-7b-instruct"},
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	got, err := c.RuntimeGetOrchestra(core.RuntimeGetOrchestraParams{})
	if err != nil {
		t.Fatal(err)
	}
	// planner + lead (L4) + complex/focused/micro + embeddings
	if len(got.Roles) != 6 {
		t.Fatalf("roles=%d", len(got.Roles))
	}
	if got.Roles[1].Key != "lead" || got.Roles[1].Tier != "L4" {
		t.Fatalf("lead role: %+v", got.Roles[1])
	}
	if got.Roles[5].Key != "embed" {
		t.Fatalf("embed role: %+v", got.Roles[5])
	}
	if got.Roles[2].Model != "openai/gpt-4o" || len(got.Roles[2].Models) != 2 {
		t.Fatalf("complex tier: %+v", got.Roles[2])
	}

	_, err = c.RuntimeConfigureOrchestra(core.RuntimeConfigureOrchestraParams{
		Roles: []core.RuntimeOrchestraRole{
			{Key: "planner", Provider: "openrouter", Model: "anthropic/claude-3-opus"},
			{Key: "lead", Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet"},
			{Key: "complex", Provider: "openrouter", Model: "openai/gpt-4o", Models: []string{"openai/gpt-4o", "google/gemini-pro-1.5"}},
			{Key: "focused", Provider: "openrouter", Model: "anthropic/claude-3-haiku"},
			{Key: "micro", Provider: "openrouter", Model: "qwen/qwen-2.5-7b-instruct"},
			{Key: "embed", Provider: "openrouter", Model: "openai/text-embedding-3-small"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Orchestra.Planner.Model != "anthropic/claude-3-opus" {
		t.Fatalf("planner=%q", loaded.Orchestra.Planner.Model)
	}
	if len(loaded.Orchestra.Tiers) != 4 {
		t.Fatalf("tiers=%d", len(loaded.Orchestra.Tiers))
	}
	if loaded.Orchestra.Tiers[0].Name != "lead" || loaded.Orchestra.Tiers[0].Model != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("lead tier: %+v", loaded.Orchestra.Tiers[0])
	}
	if len(loaded.Orchestra.Tiers[1].Models) != 2 {
		t.Fatalf("complex models=%v", loaded.Orchestra.Tiers[1].Models)
	}
	// Lead binding must resolve through the standard tier resolver (worker path parity).
	if p, m, ok := loaded.ResolveTierBinding("lead"); !ok || p != "openrouter" || m != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("ResolveTierBinding(lead) = %s/%s ok=%v", p, m, ok)
	}
	if loaded.Embed.Provider != "openrouter" || loaded.Embed.Model != "openai/text-embedding-3-small" {
		t.Fatalf("embed=%+v", loaded.Embed)
	}
	resolved := loaded.ResolvedEmbed()
	if resolved.APIBase != "https://openrouter.ai/api/v1" || resolved.APIKey != "sk-test" {
		t.Fatalf("ResolvedEmbed credentials = %s / %q", resolved.APIBase, resolved.APIKey)
	}
}

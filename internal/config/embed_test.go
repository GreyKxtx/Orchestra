package config

import "testing"

func TestResolvedEmbedInheritsProviderCredentials(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.LLM.Provider = "openrouter"
	cfg.LLM.APIBase = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-main"
	cfg.Providers = map[string]LLMConfig{
		"openrouter": {
			Provider: "openrouter",
			APIBase:  "https://openrouter.ai/api/v1",
			APIKey:   "sk-or",
		},
	}
	cfg.Embed.Provider = "openrouter"
	cfg.Embed.Model = "openai/text-embedding-3-small"
	cfg.Embed.APIBase = "https://stale.ngrok-free.dev/v1"
	cfg.Embed.APIKey = "stale"

	got := cfg.ResolvedEmbed()
	if got.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("api_base=%q, want provider gateway", got.APIBase)
	}
	if got.APIKey != "sk-or" {
		t.Fatalf("api_key=%q, want provider key", got.APIKey)
	}
	if got.Model != "openai/text-embedding-3-small" {
		t.Fatalf("model=%q", got.Model)
	}
}

func TestResolvedEmbedLegacyExplicitEndpoint(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.LLM.APIBase = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-main"
	cfg.Embed.Model = "nomic-embed-text"
	cfg.Embed.APIBase = "http://127.0.0.1:1234/v1"
	cfg.Embed.APIKey = "local"

	got := cfg.ResolvedEmbed()
	if got.APIBase != "http://127.0.0.1:1234/v1" || got.APIKey != "local" {
		t.Fatalf("legacy explicit endpoint lost: %+v", got)
	}
}

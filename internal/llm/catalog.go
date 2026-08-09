package llm

import "strings"

// CatalogEntry describes one selectable LLM provider (TUI / VS Code settings).
type CatalogEntry struct {
	Key              string
	Name             string
	Category         string
	DefaultAPIBase   string
	NeedsKey         bool
	Local            bool
	EndpointEditable bool
}

// ProviderCatalog is the canonical provider list.
var ProviderCatalog = []CatalogEntry{
	{Key: "lmstudio", Name: "LM Studio", Category: "Local", DefaultAPIBase: "http://localhost:1234", Local: true, EndpointEditable: true},
	{Key: "ollama", Name: "Ollama", Category: "Local", DefaultAPIBase: "http://localhost:11434", Local: true, EndpointEditable: true},
	{Key: "vllm", Name: "vLLM", Category: "Local", DefaultAPIBase: "http://localhost:8000/v1", Local: true, EndpointEditable: true},
	{Key: "openai", Name: "OpenAI", Category: "Cloud", DefaultAPIBase: "https://api.openai.com/v1", NeedsKey: true},
	{Key: "anthropic", Name: "Anthropic", Category: "Cloud", DefaultAPIBase: "https://api.anthropic.com", NeedsKey: true},
	{Key: "google", Name: "Google Gemini", Category: "Cloud", DefaultAPIBase: "https://generativelanguage.googleapis.com/v1beta/openai", NeedsKey: true},
	{Key: "mistral", Name: "Mistral AI", Category: "Cloud", DefaultAPIBase: "https://api.mistral.ai/v1", NeedsKey: true},
	{Key: "deepseek", Name: "DeepSeek", Category: "Cloud", DefaultAPIBase: "https://api.deepseek.com/v1", NeedsKey: true},
	{Key: "xai", Name: "xAI (Grok)", Category: "Cloud", DefaultAPIBase: "https://api.x.ai/v1", NeedsKey: true},
	{Key: "moonshot", Name: "Moonshot (Kimi)", Category: "Cloud", DefaultAPIBase: "https://api.moonshot.cn/v1", NeedsKey: true},
	{Key: "openrouter", Name: "OpenRouter", Category: "Gateway", DefaultAPIBase: "https://openrouter.ai/api/v1", NeedsKey: true},
	{Key: "groq", Name: "Groq", Category: "Gateway", DefaultAPIBase: "https://api.groq.com/openai/v1", NeedsKey: true},
	{Key: "together", Name: "Together AI", Category: "Gateway", DefaultAPIBase: "https://api.together.xyz/v1", NeedsKey: true},
	{Key: "fireworks", Name: "Fireworks AI", Category: "Gateway", DefaultAPIBase: "https://api.fireworks.ai/inference/v1", NeedsKey: true},
	{Key: "cerebras", Name: "Cerebras", Category: "Gateway", DefaultAPIBase: "https://api.cerebras.ai/v1", NeedsKey: true},
	{Key: "custom", Name: "Custom (OpenAI-compatible)", Category: "Other", Local: true, EndpointEditable: true},
}

// FindCatalogProvider returns a catalog entry by key.
func FindCatalogProvider(key string) (CatalogEntry, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, p := range ProviderCatalog {
		if p.Key == key {
			return p, true
		}
	}
	return CatalogEntry{}, false
}

// CatalogKeys returns all catalog provider keys (excluding custom).
func CatalogKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(ProviderCatalog))
	for _, p := range ProviderCatalog {
		out[p.Key] = struct{}{}
	}
	return out
}

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

// IsKnownCloudEndpoint reports whether apiBase is the endpoint of a hosted
// provider Orchestra ships in ProviderCatalog.
//
// It is an allowlist, not a "not localhost" check, and deliberately so: it
// gates the built-in price table, and the cost of a false positive is an
// invented dollar figure in usage.jsonl. A self-hosted vLLM served through a
// public tunnel has a public host but no bill; a local model named
// "qwen3-coder" would otherwise inherit the hosted model's price.
func IsKnownCloudEndpoint(apiBase string) bool {
	host := endpointHost(apiBase)
	if host == "" {
		return false
	}
	for _, p := range ProviderCatalog {
		if p.Local || p.DefaultAPIBase == "" {
			continue
		}
		if h := endpointHost(p.DefaultAPIBase); h != "" && h == host {
			return true
		}
	}
	return false
}

// endpointHost extracts the lowercase host from a URL, tolerating a missing
// scheme, a trailing path and a port.
func endpointHost(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:] // strip userinfo
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i] // strip port
	}
	if strings.ContainsAny(s, " \t") || !strings.Contains(s, ".") {
		return ""
	}
	return s
}

// CatalogKeys returns all catalog provider keys (excluding custom).
func CatalogKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(ProviderCatalog))
	for _, p := range ProviderCatalog {
		out[p.Key] = struct{}{}
	}
	return out
}

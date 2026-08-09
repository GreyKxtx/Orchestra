package llm

import (
	"strings"
)

// ProviderRegistry holds the default LLM config and named provider overrides.
type ProviderRegistry struct {
	Default LLMConfig
	Named   map[string]LLMConfig
}

// NewProviderRegistry builds a registry from project-level llm + providers maps.
func NewProviderRegistry(defaultLLM LLMConfig, named map[string]LLMConfig) ProviderRegistry {
	return ProviderRegistry{Default: defaultLLM, Named: named}
}

// FindProvider returns a named providers: entry.
func (r ProviderRegistry) FindProvider(name string) (LLMConfig, bool) {
	if r.Named == nil {
		return LLMConfig{}, false
	}
	cfg, ok := r.Named[name]
	if !ok {
		return LLMConfig{}, false
	}
	return cfg, true
}

// ResolveProviderConfig returns LLM credentials for a catalog key or named providers: entry.
func ResolveProviderConfig(reg ProviderRegistry, key string) (LLMConfig, CatalogEntry, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return reg.Default, CatalogEntry{}, true
	}
	if named, ok := reg.FindProvider(key); ok {
		if strings.TrimSpace(named.Provider) == "" {
			named.Provider = key
		}
		cat, _ := FindCatalogProvider(key)
		return named, cat, true
	}
	cat, catOK := FindCatalogProvider(key)
	if !catOK {
		return LLMConfig{}, CatalogEntry{}, false
	}
	cfg := LLMConfig{Provider: key, APIBase: cat.DefaultAPIBase}
	if strings.EqualFold(key, strings.TrimSpace(reg.Default.Provider)) {
		cfg = reg.Default
		cfg.Provider = key
	}
	if strings.TrimSpace(cfg.APIBase) == "" {
		cfg.APIBase = cat.DefaultAPIBase
	}
	return cfg, cat, true
}

// ProviderConfigured reports whether credentials exist for probe/list_models.
func ProviderConfigured(cat CatalogEntry, cfg LLMConfig) bool {
	base := strings.TrimSpace(cfg.APIBase)
	if base == "" {
		base = cat.DefaultAPIBase
	}
	if base == "" {
		return false
	}
	if cat.NeedsKey && strings.TrimSpace(cfg.APIKey) == "" {
		return false
	}
	return true
}

// ProviderDisplayName returns a human label for a provider key.
func ProviderDisplayName(key string, cat CatalogEntry, named bool) string {
	if named {
		return key + " (named)"
	}
	if cat.Name != "" {
		return cat.Name
	}
	return key
}

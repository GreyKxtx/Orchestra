package llm

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
)

// ResolveProviderConfig returns LLM credentials for a catalog key or named providers: entry.
func ResolveProviderConfig(c *config.ProjectConfig, key string) (config.LLMConfig, CatalogEntry, bool) {
	if c == nil {
		return config.LLMConfig{}, CatalogEntry{}, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return c.LLM, CatalogEntry{}, true
	}
	if named, ok := c.FindProvider(key); ok {
		if strings.TrimSpace(named.Provider) == "" {
			named.Provider = key
		}
		cat, _ := FindCatalogProvider(key)
		return named, cat, true
	}
	cat, catOK := FindCatalogProvider(key)
	if !catOK {
		return config.LLMConfig{}, CatalogEntry{}, false
	}
	cfg := config.LLMConfig{Provider: key, APIBase: cat.DefaultAPIBase}
	if strings.EqualFold(key, strings.TrimSpace(c.LLM.Provider)) {
		cfg = c.LLM
		cfg.Provider = key
	}
	if strings.TrimSpace(cfg.APIBase) == "" {
		cfg.APIBase = cat.DefaultAPIBase
	}
	return cfg, cat, true
}

// ProviderConfigured reports whether credentials exist for probe/list_models.
func ProviderConfigured(cat CatalogEntry, cfg config.LLMConfig) bool {
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

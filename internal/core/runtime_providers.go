package core

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
)

// RuntimeListProvidersParams lists catalog + named providers.
type RuntimeListProvidersParams struct {
	// Probe fetches /models for each configured (ready) provider. Default false.
	Probe *bool `json:"probe,omitempty"`
	// ProbeKey limits probe to one provider key (catalog or named).
	ProbeKey string `json:"probe_key,omitempty"`
}

// RuntimeProviderEntry is one selectable provider in settings UI.
type RuntimeProviderEntry struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Category     string              `json:"category"`
	APIBase      string              `json:"api_base"`
	Active       bool                `json:"active"`
	Ready        bool                `json:"ready"`
	Configured   bool                `json:"configured"`
	APIKeySet    bool                `json:"api_key_set"`
	NeedsKey     bool                `json:"needs_key"`
	Named        bool                `json:"named"`
	Custom       bool                `json:"custom"`
	CurrentModel string              `json:"current_model,omitempty"`
	Models       []RuntimeModelEntry `json:"models,omitempty"`
	ModelsError  string              `json:"models_error,omitempty"`
	ModelCount   int                 `json:"model_count"`
}

// RuntimeListProvidersResult is returned by runtime.list_providers.
type RuntimeListProvidersResult struct {
	Providers      []RuntimeProviderEntry `json:"providers"`
	ActiveProvider string               `json:"active_provider"`
	ActiveModel    string               `json:"active_model"`
}

// RuntimeListProviders returns the provider catalog + named entries with optional probe.
func (c *Core) RuntimeListProviders(ctx context.Context, params RuntimeListProvidersParams) (*RuntimeListProvidersResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	probe := false
	if params.Probe != nil {
		probe = *params.Probe
	}
	probeKey := strings.TrimSpace(params.ProbeKey)
	active := strings.TrimSpace(c.cfg.LLM.Provider)

	seen := make(map[string]struct{})
	out := make([]RuntimeProviderEntry, 0, len(llm.ProviderCatalog)+len(c.cfg.Providers))

	addEntry := func(key string, cat llm.CatalogEntry, named bool, custom bool) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}

		llmCfg, catEntry, ok := llm.ResolveProviderConfig(c.cfg, key)
		if !ok {
			return
		}
		if cat.Key == "" {
			cat = catEntry
		}
		displayBase := strings.TrimSpace(llmCfg.APIBase)
		if displayBase == "" {
			displayBase = cat.DefaultAPIBase
		}
		ready := llm.ProviderConfigured(cat, llmCfg)
		configured := providerSavedInConfig(c.cfg, key, cat, llmCfg)
		entry := RuntimeProviderEntry{
			Key:          key,
			Name:         llm.ProviderDisplayName(key, cat, named),
			Category:     cat.Category,
			APIBase:      displayBase,
			Active:       strings.EqualFold(key, active),
			Ready:        ready,
			Configured:   configured,
			APIKeySet:    strings.TrimSpace(llmCfg.APIKey) != "",
			NeedsKey:     cat.NeedsKey,
			Named:        named,
			Custom:       custom,
			CurrentModel: strings.TrimSpace(llmCfg.Model),
		}
		if entry.Active {
			entry.CurrentModel = strings.TrimSpace(c.cfg.LLM.Model)
		}
		out = append(out, entry)
	}

	for _, cat := range llm.ProviderCatalog {
		addEntry(cat.Key, cat, false, cat.Key == "custom")
	}

	catalogKeys := llm.CatalogKeys()
	namedKeys := make([]string, 0, len(c.cfg.Providers))
	for name := range c.cfg.Providers {
		namedKeys = append(namedKeys, name)
	}
	sort.Strings(namedKeys)
	for _, name := range namedKeys {
		if _, inCat := catalogKeys[name]; inCat {
			continue
		}
		addEntry(name, llm.CatalogEntry{
			Key:      name,
			Name:     name,
			Category: "Named",
		}, true, false)
	}

	if probe || probeKey != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		for i := range out {
			key := out[i].Key
			if probeKey != "" && !strings.EqualFold(key, probeKey) {
				continue
			}
			if !probe && probeKey == "" {
				continue
			}
			if !out[i].Ready {
				continue
			}
			if probeKey == "" && !out[i].Configured {
				continue
			}
			models, err := c.listModelsForProvider(probeCtx, key)
			if err != nil {
				out[i].ModelsError = err.Error()
				continue
			}
			out[i].Models = models
			out[i].ModelCount = len(models)
		}
	}

	return &RuntimeListProvidersResult{
		Providers:      out,
		ActiveProvider: active,
		ActiveModel:    strings.TrimSpace(c.cfg.LLM.Model),
	}, nil
}

func (c *Core) listModelsForProvider(ctx context.Context, key string) ([]RuntimeModelEntry, error) {
	llmCfg, _, ok := llm.ResolveProviderConfig(c.cfg, key)
	if !ok {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown provider", map[string]any{"provider": key})
	}
	remote, err := llm.ListRemoteModels(ctx, llmCfg)
	if err != nil {
		return nil, err
	}
	out := make([]RuntimeModelEntry, 0, len(remote))
	for _, m := range remote {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		out = append(out, RuntimeModelEntry{ID: id, OwnedBy: m.OwnedBy})
	}
	return out, nil
}

func providerSavedInConfig(c *config.ProjectConfig, key string, cat llm.CatalogEntry, llmCfg config.LLMConfig) bool {
	if c == nil {
		return false
	}
	if strings.EqualFold(key, strings.TrimSpace(c.LLM.Provider)) {
		return true
	}
	if _, ok := c.FindProvider(key); ok {
		return true
	}
	if cat.NeedsKey && strings.TrimSpace(llmCfg.APIKey) != "" {
		return true
	}
	if cat.Local {
		base := strings.TrimSpace(llmCfg.APIBase)
		if base != "" && !strings.EqualFold(base, strings.TrimSpace(cat.DefaultAPIBase)) {
			return true
		}
	}
	return false
}

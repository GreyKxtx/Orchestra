package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// RuntimeOrchestraRoleKey identifies planner or worker tier rows.
type RuntimeOrchestraRoleKey string

const (
	orchestraRolePlanner RuntimeOrchestraRoleKey = "planner"
	orchestraRoleLead    RuntimeOrchestraRoleKey = "lead"
	orchestraRoleComplex RuntimeOrchestraRoleKey = "complex"
	orchestraRoleFocused RuntimeOrchestraRoleKey = "focused"
	orchestraRoleMicro   RuntimeOrchestraRoleKey = "micro"
	orchestraRoleEmbed   RuntimeOrchestraRoleKey = "embed"
)

// RuntimeOrchestraRole is one editable orchestra role row.
type RuntimeOrchestraRole struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Tier     string   `json:"tier,omitempty"` // canonical L1–L5 tier (spec §1.4 / legacy_map)
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Models   []string `json:"models,omitempty"`
}

// RuntimeOrchestraNamedProvider is a named providers: entry snapshot for UI.
type RuntimeOrchestraNamedProvider struct {
	Key        string `json:"key"`
	APIBase    string `json:"api_base,omitempty"`
	APIKeySet  bool   `json:"api_key_set"`
	Model      string `json:"model,omitempty"`
	NeedsKey   bool   `json:"needs_key"`
	Label      string `json:"label,omitempty"`
	Configured bool   `json:"configured"`
}

// RuntimeGetOrchestraParams is empty — reads current .orchestra.yml orchestra block.
type RuntimeGetOrchestraParams struct{}

// RuntimeGetOrchestraResult exposes orchestra planner/tiers for settings UI.
type RuntimeGetOrchestraResult struct {
	Roles                  []RuntimeOrchestraRole                   `json:"roles"`
	DefaultTier            string                                   `json:"default_tier"`
	MaxWorkerRetries       int                                      `json:"max_worker_retries"`
	WorkerVerifyEnabled    bool                                     `json:"worker_verify_enabled"`
	MaxWorkerVerifyRetries int                                      `json:"max_worker_verify_retries"`
	WorkerLLMVerifyEnabled bool                                     `json:"worker_llm_verify_enabled"`
	MainProvider           string                                   `json:"main_provider"`
	MainModel              string                                   `json:"main_model"`
	FastProvider           string                                   `json:"fast_provider,omitempty"`
	Named                  map[string]RuntimeOrchestraNamedProvider `json:"named,omitempty"`
}

// RuntimeConfigureOrchestraProviderPatch updates one named provider snapshot.
type RuntimeConfigureOrchestraProviderPatch struct {
	Key     string `json:"key"`
	APIBase string `json:"api_base,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
}

// RuntimeConfigureOrchestraParams writes orchestra planner/tiers to .orchestra.yml.
type RuntimeConfigureOrchestraParams struct {
	Roles                  []RuntimeOrchestraRole                   `json:"roles"`
	DefaultTier            string                                   `json:"default_tier,omitempty"`
	MaxWorkerRetries       *int                                     `json:"max_worker_retries,omitempty"`
	WorkerVerifyEnabled    *bool                                    `json:"worker_verify_enabled,omitempty"`
	MaxWorkerVerifyRetries *int                                     `json:"max_worker_verify_retries,omitempty"`
	WorkerLLMVerifyEnabled *bool                                    `json:"worker_llm_verify_enabled,omitempty"`
	ProviderPatches        []RuntimeConfigureOrchestraProviderPatch `json:"provider_patches,omitempty"`
	Persist                *bool                                    `json:"persist,omitempty"`
}

// RuntimeConfigureOrchestraResult confirms save.
type RuntimeConfigureOrchestraResult struct {
	Saved bool `json:"saved"`
}

func defaultOrchestraRoles(cfg *config.ProjectConfig) []RuntimeOrchestraRole {
	roles := []RuntimeOrchestraRole{
		{Key: string(orchestraRolePlanner), Label: "Orchestrator"},
		{Key: string(orchestraRoleLead), Label: "Dept Leads"},
		{Key: string(orchestraRoleComplex), Label: "Worker · complex"},
		{Key: string(orchestraRoleFocused), Label: "Worker · focused"},
		{Key: string(orchestraRoleMicro), Label: "Worker · micro"},
		{Key: string(orchestraRoleEmbed), Label: "Embeddings"},
	}
	for i := range roles {
		roles[i].Tier = roleTierLabel(cfg, roles[i].Key)
	}
	if cfg == nil {
		return roles
	}
	roles[0].Provider = strings.TrimSpace(cfg.Orchestra.Planner.Provider)
	roles[0].Model = strings.TrimSpace(cfg.Orchestra.Planner.Model)
	if roles[0].Model != "" {
		roles[0].Models = []string{roles[0].Model}
	}
	tierMap := map[string]config.OrchestraTier{}
	for _, t := range cfg.Orchestra.Tiers {
		tierMap[strings.ToLower(strings.TrimSpace(t.Name))] = t
	}
	for i := range roles {
		if roles[i].Key == string(orchestraRolePlanner) {
			continue
		}
		if roles[i].Key == string(orchestraRoleEmbed) {
			roles[i].Provider = strings.TrimSpace(cfg.Embed.Provider)
			roles[i].Model = strings.TrimSpace(cfg.Embed.Model)
			if roles[i].Model != "" {
				roles[i].Models = []string{roles[i].Model}
			}
			continue
		}
		if t, ok := tierMap[roles[i].Key]; ok {
			roles[i].Provider = strings.TrimSpace(t.Provider)
			roles[i].Model = strings.TrimSpace(t.Model)
			if len(t.Models) > 0 {
				roles[i].Models = append([]string(nil), t.Models...)
			}
		}
	}
	return roles
}

// roleTierLabel maps a legacy role key (planner/complex/focused/micro) to its
// canonical L1–L5 tier, honoring legacy_map overrides from orchestra_routing.yaml.
func roleTierLabel(cfg *config.ProjectConfig, key string) string {
	var routing *config.OrchestraRouting
	if cfg != nil {
		routing = cfg.Routing
	}
	return routing.MapLegacy(key)
}

func orchestraNamedSnapshot(cfg *config.ProjectConfig) map[string]RuntimeOrchestraNamedProvider {
	out := map[string]RuntimeOrchestraNamedProvider{}
	if cfg == nil {
		return out
	}
	for name, pcfg := range cfg.Providers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		needs := false
		label := name
		if cat, ok := llm.FindCatalogProvider(name); ok {
			needs = cat.NeedsKey
			label = cat.Name
		} else if pcfg.Provider != "" {
			if cat, ok := llm.FindCatalogProvider(pcfg.Provider); ok {
				needs = cat.NeedsKey
				label = cat.Name
			}
		}
		out[name] = RuntimeOrchestraNamedProvider{
			Key:        name,
			APIBase:    strings.TrimSpace(pcfg.APIBase),
			APIKeySet:  strings.TrimSpace(pcfg.APIKey) != "",
			Model:      strings.TrimSpace(pcfg.Model),
			NeedsKey:   needs,
			Label:      label,
			Configured: true,
		}
	}
	for _, cat := range llm.ProviderCatalog {
		if _, ok := out[cat.Key]; ok {
			continue
		}
		out[cat.Key] = RuntimeOrchestraNamedProvider{
			Key:      cat.Key,
			APIBase:  cat.DefaultAPIBase,
			NeedsKey: cat.NeedsKey,
			Label:    cat.Name,
		}
		if cat.Key == strings.TrimSpace(cfg.LLM.Provider) {
			out[cat.Key] = RuntimeOrchestraNamedProvider{
				Key:        cat.Key,
				APIBase:    firstNonEmpty(strings.TrimSpace(cfg.LLM.APIBase), cat.DefaultAPIBase),
				APIKeySet:  strings.TrimSpace(cfg.LLM.APIKey) != "",
				Model:      strings.TrimSpace(cfg.LLM.Model),
				NeedsKey:   cat.NeedsKey,
				Label:      cat.Name,
				Configured: true,
			}
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// RuntimeGetOrchestra returns orchestra planner/tier configuration for UI.
func (c *Core) RuntimeGetOrchestra(_ RuntimeGetOrchestraParams) (*RuntimeGetOrchestraResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	o := c.cfg.Orchestra
	return &RuntimeGetOrchestraResult{
		Roles:                  defaultOrchestraRoles(c.cfg),
		DefaultTier:            o.ResolvedDefaultTier(),
		MaxWorkerRetries:       o.ResolvedMaxWorkerRetries(),
		WorkerVerifyEnabled:    o.ResolvedWorkerVerifyEnabled(),
		MaxWorkerVerifyRetries: o.ResolvedMaxWorkerVerifyRetries(),
		WorkerLLMVerifyEnabled: o.ResolvedWorkerLLMVerifyEnabled(),
		MainProvider:           strings.TrimSpace(c.cfg.LLM.Provider),
		MainModel:              strings.TrimSpace(c.cfg.LLM.Model),
		FastProvider:           strings.TrimSpace(c.cfg.LLM.Router.FastProvider),
		Named:                  orchestraNamedSnapshot(c.cfg),
	}, nil
}

// RuntimeConfigureOrchestra persists orchestra roles/tiers (parity with TUI /orchestra dialog).
func (c *Core) RuntimeConfigureOrchestra(params RuntimeConfigureOrchestraParams) (*RuntimeConfigureOrchestraResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	persist := true
	if params.Persist != nil {
		persist = *params.Persist
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	cfg := c.cfg
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.LLMConfig{}
	}

	for _, patch := range params.ProviderPatches {
		key := strings.TrimSpace(patch.Key)
		if key == "" {
			continue
		}
		pc := cfg.Providers[key]
		if pc.Provider == "" {
			pc.Provider = key
		}
		if base := strings.TrimSpace(patch.APIBase); base != "" {
			pc.APIBase = base
		}
		if k := strings.TrimSpace(patch.APIKey); k != "" {
			pc.APIKey = k
		}
		if m := strings.TrimSpace(patch.Model); m != "" {
			pc.Model = m
		}
		cfg.Providers[key] = pc
	}

	tiers := make([]config.OrchestraTier, 0, 3)
	for _, role := range params.Roles {
		key := RuntimeOrchestraRoleKey(strings.ToLower(strings.TrimSpace(role.Key)))
		prov := strings.TrimSpace(role.Provider)
		model := strings.TrimSpace(role.Model)
		models := cleanStringList(role.Models)
		if model == "" && len(models) > 0 {
			model = models[0]
		}
		if prov != "" {
			if _, ok := cfg.Providers[prov]; !ok {
				entry := config.LLMConfig{Provider: prov, Model: model}
				if cat, ok := llm.FindCatalogProvider(prov); ok {
					entry.APIBase = cat.DefaultAPIBase
				}
				cfg.Providers[prov] = entry
			} else if model != "" {
				pc := cfg.Providers[prov]
				pc.Model = model
				cfg.Providers[prov] = pc
			}
		}
		switch key {
		case orchestraRolePlanner:
			cfg.Orchestra.Planner.Provider = prov
			cfg.Orchestra.Planner.Model = model
		case orchestraRoleEmbed:
			cfg.Embed.Provider = prov
			cfg.Embed.Model = model
			// Inherit gateway credentials; drop stale embed.api_base (ngrok, etc.).
			cfg.Embed.APIBase = ""
			cfg.Embed.APIKey = ""
		case orchestraRoleLead, orchestraRoleComplex, orchestraRoleFocused, orchestraRoleMicro:
			tiers = append(tiers, config.OrchestraTier{
				Name:     string(key),
				Provider: prov,
				Model:    model,
				Models:   models,
			})
		}
	}
	if len(tiers) > 0 {
		cfg.Orchestra.Tiers = tiers
		if strings.TrimSpace(params.DefaultTier) != "" {
			cfg.Orchestra.DefaultTier = strings.TrimSpace(params.DefaultTier)
		} else if cfg.Orchestra.DefaultTier == "" {
			cfg.Orchestra.DefaultTier = "focused"
		}
	}
	if params.MaxWorkerRetries != nil && *params.MaxWorkerRetries > 0 {
		cfg.Orchestra.MaxWorkerRetries = *params.MaxWorkerRetries
	}
	if params.MaxWorkerVerifyRetries != nil && *params.MaxWorkerVerifyRetries > 0 {
		cfg.Orchestra.MaxWorkerVerifyRetries = *params.MaxWorkerVerifyRetries
	}
	if params.WorkerVerifyEnabled != nil {
		v := *params.WorkerVerifyEnabled
		cfg.Orchestra.WorkerVerifyEnabled = &v
	}
	if params.WorkerLLMVerifyEnabled != nil {
		v := *params.WorkerLLMVerifyEnabled
		cfg.Orchestra.WorkerLLMVerifyEnabled = &v
	}

	if persist {
		if err := config.Save(c.configFilePath(), cfg); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "save orchestra config: "+err.Error(), nil)
		}
		c.noteConfigMTime()
	}
	c.cfg = cfg
	c.applyEmbedRuntime()
	return &RuntimeConfigureOrchestraResult{Saved: persist}, nil
}

func (c *Core) applyEmbedRuntime() {
	if c == nil || c.tools == nil || c.cfg == nil {
		return
	}
	c.tools.SetIndexRuntime(c.cfg.ExcludeDirs, c.cfg.ResolvedEmbed())
}

func cleanStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

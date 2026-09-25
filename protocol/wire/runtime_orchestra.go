package wire

// runtime.get_orchestra, runtime.configure_orchestra.

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

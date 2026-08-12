package core

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// RuntimeSetModelParams switches the active LLM model for this core process.
// Optionally persists to .orchestra.yml (default persist=true).
type RuntimeSetModelParams struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"` // named providers: key; empty keeps current
	// Persist writes llm.model (and provider mirror) to disk. nil → true.
	Persist *bool `json:"persist,omitempty"`
}

// RuntimeSetModelResult is returned by runtime.set_model.
type RuntimeSetModelResult struct {
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	APIBase       string `json:"api_base"`
	Persisted     bool   `json:"persisted"`
	ContextTokens int    `json:"context_tokens,omitempty"`
}

// RuntimeListModelsParams selects which credential set to use for /models.
type RuntimeListModelsParams struct {
	Provider string `json:"provider,omitempty"` // empty → current llm config
}

// RuntimeModelEntry is one remote model id.
type RuntimeModelEntry struct {
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
}

// RuntimeListModelsResult is returned by runtime.list_models.
type RuntimeListModelsResult struct {
	Models   []RuntimeModelEntry `json:"models"`
	Provider string              `json:"provider"`
	APIBase  string              `json:"api_base"`
	Current  string              `json:"current"`
}

// RuntimeSetModel hot-swaps the in-process LLM client (unless injected for tests)
// and optionally persists the choice like TUI /model.
func (c *Core) RuntimeSetModel(ctx context.Context, params RuntimeSetModelParams) (*RuntimeSetModelResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	model := strings.TrimSpace(params.Model)
	if model == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "model is empty", nil)
	}

	persist := true
	if params.Persist != nil {
		persist = *params.Persist
	}

	// Serialize against agent turns that hold runMu.
	c.runMu.Lock()
	defer c.runMu.Unlock()

	llmCfg := c.cfg.LLM
	provKey := strings.TrimSpace(params.Provider)
	if provKey != "" {
		resolved, _, ok := llm.ResolveProviderConfig(c.cfg.LLMRegistry(), provKey)
		if !ok {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown provider", map[string]any{
				"provider": provKey,
			})
		}
		llmCfg = resolved
		if strings.TrimSpace(llmCfg.Provider) == "" {
			llmCfg.Provider = provKey
		}
	}
	llmCfg.Model = model

	if !c.llmClientInjected {
		client := llm.NewClient(llmCfg)
		if oc, ok := client.(*llm.OpenAIClient); ok {
			_, _ = oc.DiscoverAndApplyLimits(ctx)
		}
		c.llmClient = client
	}

	c.cfg.LLM.Model = model
	if provKey != "" {
		c.cfg.LLM.Provider = llmCfg.Provider
		if strings.TrimSpace(llmCfg.APIBase) != "" {
			c.cfg.LLM.APIBase = llmCfg.APIBase
		}
		if strings.TrimSpace(llmCfg.APIKey) != "" {
			c.cfg.LLM.APIKey = llmCfg.APIKey
		}
		if llmCfg.MaxTokens > 0 {
			c.cfg.LLM.MaxTokens = llmCfg.MaxTokens
		}
		if llmCfg.Temperature != 0 {
			c.cfg.LLM.Temperature = llmCfg.Temperature
		}
		if llmCfg.TimeoutS > 0 {
			c.cfg.LLM.TimeoutS = llmCfg.TimeoutS
		}
		if llmCfg.ExtraBody != nil {
			c.cfg.LLM.ExtraBody = llmCfg.ExtraBody
		}
	}

	// Mirror into providers map so named lookups stay consistent.
	pkey := strings.TrimSpace(c.cfg.LLM.Provider)
	if pkey != "" {
		if c.cfg.Providers == nil {
			c.cfg.Providers = map[string]config.LLMConfig{}
		}
		pc := c.cfg.Providers[pkey]
		if pc.Provider == "" {
			pc.Provider = pkey
		}
		pc.Model = c.cfg.LLM.Model
		pc.APIBase = c.cfg.LLM.APIBase
		if k := strings.TrimSpace(c.cfg.LLM.APIKey); k != "" {
			pc.APIKey = k
		}
		c.cfg.Providers[pkey] = pc
	}

	persisted := false
	cfgPath := c.configFilePath()
	if persist && strings.TrimSpace(cfgPath) != "" {
		if err := config.Save(cfgPath, c.cfg); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist model: "+err.Error(), nil)
		}
		persisted = true
		c.noteConfigMTime()
	}

	ctxTokens := 0
	if oc, ok := c.llmClient.(*llm.OpenAIClient); ok {
		ctxTokens = oc.ContextTokens()
	}

	return &RuntimeSetModelResult{
		Model:         c.cfg.LLM.Model,
		Provider:      c.cfg.LLM.Provider,
		APIBase:       c.cfg.LLM.APIBase,
		Persisted:     persisted,
		ContextTokens: ctxTokens,
	}, nil
}

// RuntimeListModels queries the configured OpenAI-compatible /models endpoint.
func (c *Core) RuntimeListModels(ctx context.Context, params RuntimeListModelsParams) (*RuntimeListModelsResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}

	llmCfg := c.cfg.LLM
	provKey := strings.TrimSpace(params.Provider)
	if provKey != "" {
		resolved, _, ok := llm.ResolveProviderConfig(c.cfg.LLMRegistry(), provKey)
		if !ok {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown provider", map[string]any{
				"provider": provKey,
			})
		}
		llmCfg = resolved
		if strings.TrimSpace(llmCfg.Provider) == "" {
			llmCfg.Provider = provKey
		}
	}

	remote, err := llm.ListRemoteModels(ctx, llmCfg)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{
			"api_base": llmCfg.APIBase,
		})
	}
	out := make([]RuntimeModelEntry, 0, len(remote))
	for _, m := range remote {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		out = append(out, RuntimeModelEntry{ID: id, OwnedBy: m.OwnedBy, ContextTokens: m.ContextTokens()})
	}
	return &RuntimeListModelsResult{
		Models:   out,
		Provider: llmCfg.Provider,
		APIBase:  llmCfg.APIBase,
		Current:  c.cfg.LLM.Model,
	}, nil
}

// RuntimeGetLLMParams is empty for now (reserved).
type RuntimeGetLLMParams struct{}

// RuntimeGetLLMResult exposes current LLM connection settings (key masked).
type RuntimeGetLLMResult struct {
	Provider       string  `json:"provider"`
	APIBase        string  `json:"api_base"`
	Model          string  `json:"model"`
	APIKeySet      bool    `json:"api_key_set"`
	APIKeyHint     string  `json:"api_key_hint,omitempty"`
	Temperature    float32 `json:"temperature"`
	MaxTokens      int     `json:"max_tokens"`
	TimeoutS       int     `json:"timeout_s"`
	PromptFamily   string  `json:"prompt_family,omitempty"`
	Multimodal     bool    `json:"multimodal"`
	NumCtx         int     `json:"num_ctx,omitempty"`
	ContextTokens  int     `json:"context_tokens,omitempty"`
}

// RuntimeConfigureLLMParams updates connection fields. Empty api_key leaves the existing key.
type RuntimeConfigureLLMParams struct {
	Provider     string   `json:"provider,omitempty"`
	APIBase      string   `json:"api_base,omitempty"`
	APIKey       string   `json:"api_key,omitempty"`
	Model        string   `json:"model,omitempty"`
	Temperature  *float32 `json:"temperature,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	TimeoutS     *int     `json:"timeout_s,omitempty"`
	PromptFamily *string  `json:"prompt_family,omitempty"`
	Multimodal     *bool    `json:"multimodal,omitempty"`
	Persist      *bool    `json:"persist,omitempty"` // default true
}

// RuntimeConfigureLLMResult mirrors set_model-ish outcome after configure.
type RuntimeConfigureLLMResult struct {
	Provider  string `json:"provider"`
	APIBase   string `json:"api_base"`
	Model     string `json:"model"`
	Persisted bool   `json:"persisted"`
	APIKeySet bool   `json:"api_key_set"`
}

// RuntimeGetLLM returns the active LLM config (secrets masked).
func (c *Core) RuntimeGetLLM(_ RuntimeGetLLMParams) (*RuntimeGetLLMResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	key := strings.TrimSpace(c.cfg.LLM.APIKey)
	ctxTok := int(c.cfg.EffectiveNumCtx())
	discCtx := 0
	if oc, ok := c.llmClient.(*llm.OpenAIClient); ok {
		discCtx = oc.ContextTokens()
	}
	return &RuntimeGetLLMResult{
		Provider:      c.cfg.LLM.Provider,
		APIBase:       c.cfg.LLM.APIBase,
		Model:         c.cfg.LLM.Model,
		APIKeySet:     key != "",
		APIKeyHint:    maskAPIKey(key),
		Temperature:   c.cfg.LLM.Temperature,
		MaxTokens:     c.cfg.LLM.MaxTokens,
		TimeoutS:      c.cfg.LLM.TimeoutS,
		PromptFamily:  c.cfg.LLM.PromptFamily,
		Multimodal:    c.cfg.LLM.Multimodal,
		NumCtx:        ctxTok,
		ContextTokens: discCtx,
	}, nil
}

// RuntimeConfigureLLM updates API base/key/model. When model is omitted, credentials
// are stored under providers:<key> without switching the active llm: client.
func (c *Core) RuntimeConfigureLLM(ctx context.Context, params RuntimeConfigureLLMParams) (*RuntimeConfigureLLMResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}

	persist := true
	if params.Persist != nil {
		persist = *params.Persist
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	targetProv := strings.TrimSpace(params.Provider)
	activeProv := strings.TrimSpace(c.cfg.LLM.Provider)
	if targetProv == "" {
		targetProv = activeProv
	}

	incomingModel := strings.TrimSpace(params.Model)
	incomingBase := strings.TrimSpace(params.APIBase)
	incomingKey := strings.TrimSpace(params.APIKey)

	if targetProv != "" {
		if c.cfg.Providers == nil {
			c.cfg.Providers = map[string]config.LLMConfig{}
		}
		pc := c.cfg.Providers[targetProv]
		if pc.Provider == "" {
			pc.Provider = targetProv
		}
		if incomingBase != "" {
			pc.APIBase = incomingBase
		} else if strings.TrimSpace(pc.APIBase) == "" {
			if cat, ok := llm.FindCatalogProvider(targetProv); ok {
				pc.APIBase = cat.DefaultAPIBase
			}
		}
		if incomingKey != "" {
			pc.APIKey = incomingKey
		}
		if incomingModel != "" {
			pc.Model = incomingModel
		}
		if params.Temperature != nil {
			pc.Temperature = *params.Temperature
		}
		if params.MaxTokens != nil && *params.MaxTokens > 0 {
			pc.MaxTokens = *params.MaxTokens
		}
		if params.TimeoutS != nil && *params.TimeoutS > 0 {
			pc.TimeoutS = *params.TimeoutS
		}
		if params.Multimodal != nil {
			pc.Multimodal = *params.Multimodal
		}
		c.cfg.Providers[targetProv] = pc
	}

	shouldActivate := incomingModel != ""
	if !shouldActivate && strings.TrimSpace(params.Provider) != "" && strings.EqualFold(targetProv, activeProv) {
		// Active provider tuning / key rotation — keep current model.
		shouldActivate = strings.TrimSpace(c.cfg.LLM.Model) != "" || incomingModel != ""
	}
	if !shouldActivate && strings.TrimSpace(params.Provider) == "" {
		shouldActivate = incomingModel != "" || strings.TrimSpace(c.cfg.LLM.Model) != ""
	}

	if shouldActivate {
		if strings.TrimSpace(params.Provider) != "" {
			c.cfg.LLM.Provider = targetProv
		}
		pc := c.cfg.LLM
		if entry, ok := c.cfg.Providers[targetProv]; ok {
			pc = mergeLLMFromProvider(entry, pc)
		}
		if incomingBase != "" {
			pc.APIBase = incomingBase
		}
		if incomingKey != "" {
			pc.APIKey = incomingKey
		}
		if incomingModel != "" {
			pc.Model = incomingModel
		}
		if params.Temperature != nil {
			pc.Temperature = *params.Temperature
		}
		if params.MaxTokens != nil && *params.MaxTokens > 0 {
			pc.MaxTokens = *params.MaxTokens
		}
		if params.TimeoutS != nil && *params.TimeoutS > 0 {
			pc.TimeoutS = *params.TimeoutS
		}
		if params.PromptFamily != nil {
			pc.PromptFamily = strings.TrimSpace(*params.PromptFamily)
		}
		if params.Multimodal != nil {
			pc.Multimodal = *params.Multimodal
		}
		c.cfg.LLM = pc

		if strings.TrimSpace(c.cfg.LLM.APIBase) == "" {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "api_base is empty", nil)
		}
		if strings.TrimSpace(c.cfg.LLM.Model) == "" {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "model is empty", nil)
		}

		if !c.llmClientInjected {
			client := llm.NewClient(c.cfg.LLM)
			if oc, ok := client.(*llm.OpenAIClient); ok {
				_, _ = oc.DiscoverAndApplyLimits(ctx)
			}
			c.llmClient = client
		}

		if targetProv != "" {
			pc := c.cfg.Providers[targetProv]
			if pc.Provider == "" {
				pc.Provider = targetProv
			}
			pc.APIBase = c.cfg.LLM.APIBase
			if k := strings.TrimSpace(c.cfg.LLM.APIKey); k != "" {
				pc.APIKey = k
			}
			pc.Model = c.cfg.LLM.Model
			pc.Temperature = c.cfg.LLM.Temperature
			pc.MaxTokens = c.cfg.LLM.MaxTokens
			pc.Multimodal = c.cfg.LLM.Multimodal
			if c.cfg.LLM.TimeoutS > 0 {
				pc.TimeoutS = c.cfg.LLM.TimeoutS
			}
			c.cfg.Providers[targetProv] = pc
		}
	}

	persisted := false
	cfgPath := c.configFilePath()
	if persist && strings.TrimSpace(cfgPath) != "" {
		if err := config.Save(cfgPath, c.cfg); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist llm config: "+err.Error(), nil)
		}
		persisted = true
		c.noteConfigMTime()
	}

	keySet := strings.TrimSpace(c.cfg.LLM.APIKey) != ""
	if targetProv != "" {
		if pc, ok := c.cfg.Providers[targetProv]; ok && strings.TrimSpace(pc.APIKey) != "" {
			keySet = true
		}
	}

	return &RuntimeConfigureLLMResult{
		Provider:  firstNonEmpty(targetProv, c.cfg.LLM.Provider),
		APIBase:   firstNonEmpty(incomingBase, c.cfg.LLM.APIBase),
		Model:     firstNonEmpty(incomingModel, c.cfg.LLM.Model),
		Persisted: persisted,
		APIKeySet: keySet,
	}, nil
}

func mergeLLMFromProvider(entry config.LLMConfig, base config.LLMConfig) config.LLMConfig {
	out := base
	if strings.TrimSpace(out.Provider) == "" {
		out.Provider = entry.Provider
	}
	if strings.TrimSpace(out.APIBase) == "" && strings.TrimSpace(entry.APIBase) != "" {
		out.APIBase = entry.APIBase
	}
	if strings.TrimSpace(out.APIKey) == "" && strings.TrimSpace(entry.APIKey) != "" {
		out.APIKey = entry.APIKey
	}
	if strings.TrimSpace(out.Model) == "" && strings.TrimSpace(entry.Model) != "" {
		out.Model = entry.Model
	}
	if out.MaxTokens <= 0 && entry.MaxTokens > 0 {
		out.MaxTokens = entry.MaxTokens
	}
	if out.Temperature == 0 && entry.Temperature != 0 {
		out.Temperature = entry.Temperature
	}
	if out.TimeoutS <= 0 && entry.TimeoutS > 0 {
		out.TimeoutS = entry.TimeoutS
	}
	return out
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:3] + "…" + key[len(key)-4:]
}

func (c *Core) configFilePath() string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.configPath) != "" {
		return c.configPath
	}
	if strings.TrimSpace(c.workspaceRoot) == "" {
		return ""
	}
	return filepath.Join(c.workspaceRoot, ".orchestra.yml")
}

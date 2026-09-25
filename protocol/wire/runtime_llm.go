package wire

import ()

// runtime.set_model, runtime.list_models, runtime.get_llm, runtime.configure_llm, runtime.credits.

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

// RuntimeCreditsParams selects which provider's balance to query.
// Empty Provider uses the primary llm config.
type RuntimeCreditsParams struct {
	Provider string `json:"provider,omitempty"`
}

// RuntimeCreditsResult is returned by runtime.credits. Supported=false means
// the provider has no balance API we know (local servers, plain OpenAI base).
type RuntimeCreditsResult struct {
	Provider     string  `json:"provider"`
	Supported    bool    `json:"supported"`
	TotalCredits float64 `json:"total_credits,omitempty"`
	TotalUsage   float64 `json:"total_usage,omitempty"`
	Balance      float64 `json:"balance,omitempty"`
}

// RuntimeGetLLMParams is empty for now (reserved).
type RuntimeGetLLMParams struct{}

// RuntimeGetLLMResult exposes current LLM connection settings (key masked).
type RuntimeGetLLMResult struct {
	Provider      string  `json:"provider"`
	APIBase       string  `json:"api_base"`
	Model         string  `json:"model"`
	APIKeySet     bool    `json:"api_key_set"`
	APIKeyHint    string  `json:"api_key_hint,omitempty"`
	Temperature   float32 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
	TimeoutS      int     `json:"timeout_s"`
	PromptFamily  string  `json:"prompt_family,omitempty"`
	Multimodal    bool    `json:"multimodal"`
	NumCtx        int     `json:"num_ctx,omitempty"`
	ContextTokens int     `json:"context_tokens,omitempty"`
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
	Multimodal   *bool    `json:"multimodal,omitempty"`
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

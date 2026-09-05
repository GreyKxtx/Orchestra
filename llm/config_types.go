package llm

// LLMConfig contains LLM API settings (YAML: llm / providers in .orchestra.yml).
type LLMConfig struct {
	Provider    string  `yaml:"provider"`
	APIBase     string  `yaml:"api_base"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float32 `yaml:"temperature"`
	TimeoutS    int     `yaml:"timeout_s"`

	PromptFamily string `yaml:"prompt_family"`

	Multimodal bool `yaml:"multimodal,omitempty"`

	ResponseFormatType string `yaml:"response_format_type"`
	SupportsJSONSchema *bool  `yaml:"supports_json_schema,omitempty"`
	ToolChoice         string `yaml:"tool_choice,omitempty"`

	ExtraBody map[string]any `yaml:"extra_body,omitempty"`

	// Azure switches the OpenAI-compatible client to Azure OpenAI's dialect:
	// the deployment-scoped URL and the api-key header. Provider "azure"
	// enables it on its own; set this to name a deployment or pin a version.
	Azure *AzureConfig `yaml:"azure,omitempty"`

	ModelPresets map[string]ModelPreset `yaml:"model_presets,omitempty"`
	Router       RouterConfig           `yaml:"router,omitempty"`
}

// AzureConfig holds the two things Azure OpenAI needs beyond api_base and
// api_key. Both are optional: Deployment falls back to the model name (what
// the portal names a deployment by default) and APIVersion to a GA release.
type AzureConfig struct {
	Deployment string `yaml:"deployment,omitempty"`
	APIVersion string `yaml:"api_version,omitempty"`
}

// RouterConfig configures RouterClient. Disabled when Enabled is false or FastProvider is empty.
type RouterConfig struct {
	Enabled        bool   `yaml:"enabled"`
	FastProvider   string `yaml:"fast_provider"`
	ThresholdBytes int    `yaml:"threshold_bytes"`
}

// ModelPreset captures settings associated with one model id (TUI model switching).
type ModelPreset struct {
	Provider       string  `yaml:"provider,omitempty"`
	APIBase        string  `yaml:"api_base,omitempty"`
	Temperature    float32 `yaml:"temperature,omitempty"`
	MaxTokens      int     `yaml:"max_tokens,omitempty"`
	NumCtx         int64   `yaml:"num_ctx,omitempty"`
	EnableThinking *bool   `yaml:"enable_thinking,omitempty"`
}

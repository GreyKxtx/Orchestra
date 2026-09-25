package wire

// runtime.get_system_prompt, runtime.set_system_prompt.

// RuntimeGetSystemPromptParams is reserved.
type RuntimeGetSystemPromptParams struct{}

// RuntimeGetSystemPromptResult exposes .orchestra/system.txt + prompt_family.
type RuntimeGetSystemPromptResult struct {
	Content      string `json:"content"`
	HasOverride  bool   `json:"has_override"`
	PromptFamily string `json:"prompt_family"`
	Path         string `json:"path"`
}

// RuntimeSetSystemPromptParams writes or clears the system override.
type RuntimeSetSystemPromptParams struct {
	Content      *string `json:"content,omitempty"`       // nil = leave file; "" = clear
	Clear        bool    `json:"clear,omitempty"`         // force delete override
	PromptFamily *string `json:"prompt_family,omitempty"` // set llm.prompt_family when non-nil
	Persist      *bool   `json:"persist,omitempty"`       // persist prompt_family to yaml; default true
}

// RuntimeSetSystemPromptResult confirms write.
type RuntimeSetSystemPromptResult struct {
	HasOverride  bool   `json:"has_override"`
	PromptFamily string `json:"prompt_family"`
	Persisted    bool   `json:"persisted"`
	Path         string `json:"path"`
}

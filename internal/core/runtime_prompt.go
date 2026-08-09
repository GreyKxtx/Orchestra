package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/protocol"
)

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

// RuntimeGetSystemPrompt returns the workspace system override text.
func (c *Core) RuntimeGetSystemPrompt(_ RuntimeGetSystemPromptParams) (*RuntimeGetSystemPromptResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	content := prompt.LoadSystemOverride(c.workspaceRoot)
	return &RuntimeGetSystemPromptResult{
		Content:      content,
		HasOverride:  content != "",
		PromptFamily: c.cfg.LLM.PromptFamily,
		Path:         prompt.SystemOverridePath(c.workspaceRoot),
	}, nil
}

// RuntimeSetSystemPrompt writes/clears .orchestra/system.txt and optional prompt_family.
func (c *Core) RuntimeSetSystemPrompt(params RuntimeSetSystemPromptParams) (*RuntimeSetSystemPromptResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()

	if params.Clear {
		if err := prompt.WriteSystemOverride(c.workspaceRoot, ""); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "clear system.txt: "+err.Error(), nil)
		}
	} else if params.Content != nil {
		if err := prompt.WriteSystemOverride(c.workspaceRoot, *params.Content); err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "write system.txt: "+err.Error(), nil)
		}
	}

	persisted := false
	if params.PromptFamily != nil {
		c.cfg.LLM.PromptFamily = strings.TrimSpace(*params.PromptFamily)
		if persistDefaultTrue(params.Persist) {
			ok, err := c.saveConfigLocked()
			if err != nil {
				return nil, protocol.NewError(protocol.ExecFailed, "failed to persist prompt_family: "+err.Error(), nil)
			}
			persisted = ok
		}
	}

	content := prompt.LoadSystemOverride(c.workspaceRoot)
	return &RuntimeSetSystemPromptResult{
		HasOverride:  content != "",
		PromptFamily: c.cfg.LLM.PromptFamily,
		Persisted:    persisted,
		Path:         prompt.SystemOverridePath(c.workspaceRoot),
	}, nil
}

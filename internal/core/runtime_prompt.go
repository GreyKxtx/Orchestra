package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

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

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	RuntimeGetSystemPromptParams = wire.RuntimeGetSystemPromptParams
	RuntimeGetSystemPromptResult = wire.RuntimeGetSystemPromptResult
	RuntimeSetSystemPromptParams = wire.RuntimeSetSystemPromptParams
	RuntimeSetSystemPromptResult = wire.RuntimeSetSystemPromptResult
)

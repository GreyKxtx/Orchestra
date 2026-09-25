package core

import (
	"context"
	"reflect"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// WorkspaceTrustParams is the workspace.trust request. Revoke forgets the
// workspace instead of trusting it.
type WorkspaceTrustParams struct {
	Revoke bool `json:"revoke,omitempty"`
}

// WorkspaceTrustResult is the workspace's trust state (config.WorkspaceTrust)
// plus the warnings of restarting its MCP servers after a change.
type WorkspaceTrustResult struct {
	config.WorkspaceTrust
	Warnings []string `json:"warnings,omitempty"`
}

// WorkspaceTrustStatus serves workspace.trust_status: whether the workspace's
// own machine-level settings are in effect, and which are ignored if not.
func (c *Core) WorkspaceTrustStatus() (*WorkspaceTrustResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	st, err := config.WorkspaceTrustStatus(c.configFilePath())
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &WorkspaceTrustResult{WorkspaceTrust: st}, nil
}

// WorkspaceTrust serves workspace.trust: it records (or, with revoke,
// forgets) the workspace's current settings as trusted and reloads the
// config, so its MCP servers, hooks and consent settings take effect — or
// stop — without restarting the core. A turn in flight is not interrupted:
// the call is refused until it ends.
func (c *Core) WorkspaceTrust(ctx context.Context, p WorkspaceTrustParams) (*WorkspaceTrustResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	path := c.configFilePath()
	if !c.runMu.TryLock() {
		return nil, protocol.NewError(protocol.ExecFailed, "a turn is running; change workspace trust after it ends", nil)
	}
	defer c.runMu.Unlock()

	var err error
	if p.Revoke {
		err = config.UntrustWorkspace(path)
	} else {
		_, err = config.TrustWorkspace(path)
	}
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	fresh, err := config.Load(path)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, "reload config: "+err.Error(), nil)
	}

	c.cfgMu.Lock()
	oldLLM := c.cfg.LLM
	c.cfg = fresh
	warnings := c.ReplaceMCP(ctx, fresh.MCP)
	c.cfgMu.Unlock()
	if !c.llmClientInjected && !reflect.DeepEqual(oldLLM, fresh.LLM) {
		c.llmClient = llm.BuildClient(fresh.LLM, fresh.LLMRegistry(), llm.NewLogger(c.workspaceRoot))
	}
	c.publishSamplingTarget()
	c.applyEmbedRuntime()
	c.noteConfigMTime()
	return &WorkspaceTrustResult{WorkspaceTrust: fresh.Trust(), Warnings: warnings}, nil
}

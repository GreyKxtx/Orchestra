package core

import (
	"context"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/protocol"
)

// MCPServerParams is the JSON shape for one MCP server (upsert / test).
type MCPServerParams struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	Disabled     bool              `json:"disabled,omitempty"`
	CallTimeoutS int               `json:"call_timeout_s,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
}

// MCPListParams is reserved.
type MCPListParams struct{}

// MCPServerView is one row in mcp.list.
type MCPServerView struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	Disabled     bool              `json:"disabled"`
	CallTimeoutS int               `json:"call_timeout_s,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
	Status       string            `json:"status"` // running | disabled | error | stopped
	ToolCount    int               `json:"tool_count"`
	Error        string            `json:"error,omitempty"`
}

// MCPListResult is returned by mcp.list.
type MCPListResult struct {
	Servers []MCPServerView `json:"servers"`
}

// MCPUpsertParams adds or replaces a server by name.
type MCPUpsertParams struct {
	Server  MCPServerParams `json:"server"`
	Persist *bool           `json:"persist,omitempty"` // default true
}

// MCPUpsertResult is returned after upsert + hot reload.
type MCPUpsertResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPDeleteParams removes a server by name.
type MCPDeleteParams struct {
	Name    string `json:"name"`
	Persist *bool  `json:"persist,omitempty"`
}

// MCPDeleteResult mirrors list after delete.
type MCPDeleteResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPSetDisabledParams toggles disabled.
type MCPSetDisabledParams struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Persist  *bool  `json:"persist,omitempty"`
}

// MCPSetDisabledResult mirrors list after toggle.
type MCPSetDisabledResult struct {
	Servers   []MCPServerView `json:"servers"`
	Persisted bool            `json:"persisted"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// MCPTestParams probes a server config (or named cfg entry) without persisting.
type MCPTestParams struct {
	Name   string           `json:"name,omitempty"`   // use existing cfg entry
	Server *MCPServerParams `json:"server,omitempty"` // or ad-hoc config
}

// MCPTestResult lists tools from a temporary connection.
type MCPTestResult struct {
	OK      bool     `json:"ok"`
	Name    string   `json:"name"`
	Tools   []string `json:"tools,omitempty"`
	Error   string   `json:"error,omitempty"`
	Elapsed string   `json:"elapsed,omitempty"`
}

// ReplaceMCP hot-swaps the MCP manager and rebinds tools.Runner. Caller must hold runMu.
func (c *Core) ReplaceMCP(ctx context.Context, mcpCfg config.MCPConfig) []string {
	if c == nil {
		return nil
	}
	if c.mcpManager != nil {
		c.mcpManager.Close()
		c.mcpManager = nil
	}
	var warnings []string
	c.mcpStartErrs = map[string]string{}
	if len(mcpCfg.Servers) == 0 {
		if c.tools != nil {
			c.tools.SetMCPCaller(nil)
		}
		c.cfg.MCP = mcpCfg
		return nil
	}
	mgr, startErrs := mcp.NewManager(ctx, mcpCfg)
	for _, err := range startErrs {
		warnings = append(warnings, err.Error())
		name := extractMCPErrName(err.Error())
		if name != "" {
			c.mcpStartErrs[name] = err.Error()
		}
	}
	c.mcpManager = mgr
	if c.tools != nil {
		if mgr != nil && !mgr.IsEmpty() {
			c.tools.SetMCPCaller(mgr)
		} else {
			c.tools.SetMCPCaller(nil)
		}
	}
	c.cfg.MCP = mcpCfg
	return warnings
}

func extractMCPErrName(msg string) string {
	const prefix = `mcp server "`
	if !strings.HasPrefix(msg, prefix) {
		return ""
	}
	rest := msg[len(prefix):]
	i := strings.Index(rest, `"`)
	if i <= 0 {
		return ""
	}
	return rest[:i]
}

func (c *Core) mcpViews() []MCPServerView {
	runtime := map[string]mcp.ServerStatus{}
	if c.mcpManager != nil {
		for _, s := range c.mcpManager.RuntimeStatuses() {
			runtime[s.Name] = s
		}
	}
	out := make([]MCPServerView, 0, len(c.cfg.MCP.Servers))
	for _, srv := range c.cfg.MCP.Servers {
		v := MCPServerView{
			Name:         srv.Name,
			Command:      append([]string(nil), srv.Command...),
			Env:          cloneStringMap(srv.Env),
			Disabled:     srv.Disabled,
			CallTimeoutS: srv.CallTimeoutS,
			AllowedTools: append([]string(nil), srv.AllowedTools...),
			Status:       "stopped",
		}
		if srv.Disabled {
			v.Status = "disabled"
		} else if rs, ok := runtime[srv.Name]; ok {
			if rs.Dead {
				v.Status = "error"
				v.Error = "subprocess dead"
			} else {
				v.Status = "running"
				v.ToolCount = rs.ToolCount
			}
		} else if errMsg, ok := c.mcpStartErrs[srv.Name]; ok {
			v.Status = "error"
			v.Error = errMsg
		} else if len(srv.Command) == 0 {
			v.Status = "error"
			v.Error = "command is empty"
		}
		out = append(out, v)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func serverFromParams(p MCPServerParams) config.MCPServerConfig {
	return config.MCPServerConfig{
		Name:         strings.TrimSpace(p.Name),
		Command:      append([]string(nil), p.Command...),
		Env:          cloneStringMap(p.Env),
		Disabled:     p.Disabled,
		CallTimeoutS: p.CallTimeoutS,
		AllowedTools: append([]string(nil), p.AllowedTools...),
	}
}

func persistDefaultTrue(persist *bool) bool {
	if persist == nil {
		return true
	}
	return *persist
}

func (c *Core) saveConfigLocked() (bool, error) {
	cfgPath := c.configFilePath()
	if strings.TrimSpace(cfgPath) == "" {
		return false, nil
	}
	if err := config.Save(cfgPath, c.cfg); err != nil {
		return false, err
	}
	return true, nil
}

// MCPList returns configured servers with runtime status.
func (c *Core) MCPList(_ MCPListParams) (*MCPListResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	return &MCPListResult{Servers: c.mcpViews()}, nil
}

// MCPUpsert adds/updates a server, persists, and hot-reloads MCP.
func (c *Core) MCPUpsert(ctx context.Context, params MCPUpsertParams) (*MCPUpsertResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	srv := serverFromParams(params.Server)
	if srv.Name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "server.name is required", nil)
	}
	if strings.Contains(srv.Name, ":") {
		return nil, protocol.NewError(protocol.InvalidParams, "server.name must not contain ':'", nil)
	}
	if len(srv.Command) == 0 && !srv.Disabled {
		return nil, protocol.NewError(protocol.InvalidParams, "server.command is required", nil)
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	prev := append([]config.MCPServerConfig(nil), c.cfg.MCP.Servers...)
	found := false
	for i := range c.cfg.MCP.Servers {
		if c.cfg.MCP.Servers[i].Name == srv.Name {
			c.cfg.MCP.Servers[i] = srv
			found = true
			break
		}
	}
	if !found {
		c.cfg.MCP.Servers = append(c.cfg.MCP.Servers, srv)
	}
	if err := c.cfg.ValidateMCPOnly(); err != nil {
		c.cfg.MCP.Servers = prev
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}

	warnings := c.ReplaceMCP(ctx, c.cfg.MCP)
	persisted := false
	if persistDefaultTrue(params.Persist) {
		ok, err := c.saveConfigLocked()
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist mcp config: "+err.Error(), nil)
		}
		persisted = ok
	}
	return &MCPUpsertResult{Servers: c.mcpViews(), Persisted: persisted, Warnings: warnings}, nil
}

// MCPDelete removes a server by name.
func (c *Core) MCPDelete(ctx context.Context, params MCPDeleteParams) (*MCPDeleteResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "name is required", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()

	next := make([]config.MCPServerConfig, 0, len(c.cfg.MCP.Servers))
	found := false
	for _, s := range c.cfg.MCP.Servers {
		if s.Name == name {
			found = true
			continue
		}
		next = append(next, s)
	}
	if !found {
		return nil, protocol.NewError(protocol.InvalidParams, "mcp server not found", map[string]any{"name": name})
	}
	c.cfg.MCP.Servers = next
	warnings := c.ReplaceMCP(ctx, c.cfg.MCP)
	persisted := false
	if persistDefaultTrue(params.Persist) {
		ok, err := c.saveConfigLocked()
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist mcp config: "+err.Error(), nil)
		}
		persisted = ok
	}
	return &MCPDeleteResult{Servers: c.mcpViews(), Persisted: persisted, Warnings: warnings}, nil
}

// MCPSetDisabled toggles disabled for a named server.
func (c *Core) MCPSetDisabled(ctx context.Context, params MCPSetDisabledParams) (*MCPSetDisabledResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "name is required", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()

	found := false
	for i := range c.cfg.MCP.Servers {
		if c.cfg.MCP.Servers[i].Name == name {
			c.cfg.MCP.Servers[i].Disabled = params.Disabled
			found = true
			break
		}
	}
	if !found {
		return nil, protocol.NewError(protocol.InvalidParams, "mcp server not found", map[string]any{"name": name})
	}
	warnings := c.ReplaceMCP(ctx, c.cfg.MCP)
	persisted := false
	if persistDefaultTrue(params.Persist) {
		ok, err := c.saveConfigLocked()
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist mcp config: "+err.Error(), nil)
		}
		persisted = ok
	}
	return &MCPSetDisabledResult{Servers: c.mcpViews(), Persisted: persisted, Warnings: warnings}, nil
}

// MCPTest connects temporarily and lists tools.
func (c *Core) MCPTest(ctx context.Context, params MCPTestParams) (*MCPTestResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	var srv config.MCPServerConfig
	if params.Server != nil {
		srv = serverFromParams(*params.Server)
	} else {
		name := strings.TrimSpace(params.Name)
		if name == "" {
			return nil, protocol.NewError(protocol.InvalidParams, "name or server is required", nil)
		}
		found := false
		for _, s := range c.cfg.MCP.Servers {
			if s.Name == name {
				srv = s
				found = true
				break
			}
		}
		if !found {
			return nil, protocol.NewError(protocol.InvalidParams, "mcp server not found", map[string]any{"name": name})
		}
	}
	if srv.Name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "server.name is required", nil)
	}
	if len(srv.Command) == 0 {
		return &MCPTestResult{OK: false, Name: srv.Name, Error: "command is empty"}, nil
	}

	start := time.Now()
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var opts mcp.StartOptions
	if srv.CallTimeoutS > 0 {
		opts.CallTimeout = time.Duration(srv.CallTimeoutS) * time.Second
	}
	client, err := mcp.Start(testCtx, srv.Name, srv.Command, srv.Env, opts)
	if err != nil {
		return &MCPTestResult{
			OK:      false,
			Name:    srv.Name,
			Error:   err.Error(),
			Elapsed: time.Since(start).Round(time.Millisecond).String(),
		}, nil
	}
	defer func() { _ = client.Close() }()
	if len(srv.AllowedTools) > 0 {
		client.SetAllowedTools(srv.AllowedTools)
	}
	tools := client.Tools()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return &MCPTestResult{
		OK:      true,
		Name:    srv.Name,
		Tools:   names,
		Elapsed: time.Since(start).Round(time.Millisecond).String(),
	}, nil
}

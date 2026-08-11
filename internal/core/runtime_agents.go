package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/protocol"
)

// AgentsListParams is reserved.
type AgentsListParams struct{}

// AgentsListResult lists custom agents and built-in mode names.
type AgentsListResult struct {
	Agents          []config.AgentDefinition `json:"agents"`
	BuiltInModes    []string                 `json:"built_in_modes"`
	AvailableTools  []string                 `json:"available_tools"`
}

// AgentsList returns custom agents from cfg.
func (c *Core) AgentsList(_ AgentsListParams) (*AgentsListResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	agents := append([]config.AgentDefinition(nil), c.cfg.Agents...)
	return &AgentsListResult{
		Agents:         agents,
		BuiltInModes:   config.BuiltInModeNames(),
		AvailableTools: config.ValidAgentToolNames(),
	}, nil
}

// AgentsUpsertParams adds or replaces a custom agent.
type AgentsUpsertParams struct {
	Agent   config.AgentDefinition `json:"agent"`
	Persist *bool                  `json:"persist,omitempty"`
}

// AgentsUpsertResult is returned after save.
type AgentsUpsertResult struct {
	Agents    []config.AgentDefinition `json:"agents"`
	Persisted bool                     `json:"persisted"`
}

// AgentsDeleteParams removes a custom agent by name.
type AgentsDeleteParams struct {
	Name    string `json:"name"`
	Persist *bool  `json:"persist,omitempty"`
}

// AgentsDeleteResult is returned after delete.
type AgentsDeleteResult struct {
	Agents    []config.AgentDefinition `json:"agents"`
	Persisted bool                     `json:"persisted"`
}

// AgentsUpsert validates and saves a custom agent definition.
func (c *Core) AgentsUpsert(params AgentsUpsertParams) (*AgentsUpsertResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	agent := params.Agent
	agent.Name = strings.TrimSpace(agent.Name)
	if agent.Name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "agent.name is required", nil)
	}
	if config.IsBuiltInMode(agent.Name) {
		return nil, protocol.NewError(protocol.InvalidParams, "agent.name collides with a built-in mode", map[string]any{
			"name": agent.Name,
		})
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	prev := append([]config.AgentDefinition(nil), c.cfg.Agents...)
	found := false
	for i := range c.cfg.Agents {
		if c.cfg.Agents[i].Name == agent.Name {
			c.cfg.Agents[i] = agent
			found = true
			break
		}
	}
	if !found {
		c.cfg.Agents = append(c.cfg.Agents, agent)
	}
	// Re-run agent validation by checking via a temp ProjectConfig copy path:
	if err := c.cfg.ValidateAgentsOnly(); err != nil {
		c.cfg.Agents = prev
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}

	persisted := false
	if persistDefaultTrue(params.Persist) {
		ok, err := c.saveConfigLocked()
		if err != nil {
			c.cfg.Agents = prev
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist agents: "+err.Error(), nil)
		}
		persisted = ok
	}
	return &AgentsUpsertResult{
		Agents:    append([]config.AgentDefinition(nil), c.cfg.Agents...),
		Persisted: persisted,
	}, nil
}

// AgentsDelete removes a custom agent.
func (c *Core) AgentsDelete(params AgentsDeleteParams) (*AgentsDeleteResult, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "name is required", nil)
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()

	next := make([]config.AgentDefinition, 0, len(c.cfg.Agents))
	found := false
	for _, a := range c.cfg.Agents {
		if a.Name == name {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		return nil, protocol.NewError(protocol.InvalidParams, "agent not found", map[string]any{"name": name})
	}
	c.cfg.Agents = next
	persisted := false
	if persistDefaultTrue(params.Persist) {
		ok, err := c.saveConfigLocked()
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to persist agents: "+err.Error(), nil)
		}
		persisted = ok
	}
	return &AgentsDeleteResult{
		Agents:    append([]config.AgentDefinition(nil), c.cfg.Agents...),
		Persisted: persisted,
	}, nil
}

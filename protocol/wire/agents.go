package wire

// agents.list, agents.delete. The upsert and the results carry config.AgentDefinition and stay in internal/core.

// AgentsListParams is reserved.
type AgentsListParams struct{}

// AgentsDeleteParams removes a custom agent by name.
type AgentsDeleteParams struct {
	Name    string `json:"name"`
	Persist *bool  `json:"persist,omitempty"`
}

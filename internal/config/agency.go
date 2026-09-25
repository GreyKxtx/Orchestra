package config

import (
	"fmt"
	"regexp"
	"strings"
)

// AgencyConfig is the `agency:` section: agents as an organisation rather
// than a parent with helpers. It answers three questions the task runner used
// to hard-code — who may delegate to or message whom (flows), how deep
// delegation nests (max_depth) and how many agents run at once
// (max_parallel) — and bounds the chatter between them (max_messages).
//
// Unset, the agency is on for mode=orchestra with the spec's default edges
// (Dept Lead → Scout / Worker, Product → Scout) and off everywhere else, so
// a build-mode turn keeps its one level of read-only or scoped children.
type AgencyConfig struct {
	// Enabled forces the agency on (true) or off (false) for every mode.
	// Unset = on for mode=orchestra, and on in any mode once flows are set.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Flows are directional edges "a > b": a may delegate to b (task,
	// task_spawn) and hold a conversation with it (send_message). A chain
	// "a > b > c" adds a>b and b>c. "*" on the left means any agent. Names
	// are built-in roles, custom agents from agents:, or department types
	// (frontend, backend, …) — a department instance frontend@web matches
	// the type frontend. The root agent (orchestrator) may reach every agent
	// without an edge: it is the hub.
	Flows []string `yaml:"flows,omitempty"`
	// DefaultFlows adds the spec's edges (Dept Lead → explore / scout /
	// worker / verifier, Product → scout / explore, Documentation → explore).
	// Default true.
	DefaultFlows *bool `yaml:"default_flows,omitempty"`
	// MaxDepth caps nesting: the root is depth 0, its children 1, theirs 2.
	// Default 2 (Orchestrator → Dept Lead → Worker); range 1..4.
	MaxDepth int `yaml:"max_depth,omitempty"`
	// MaxParallel caps concurrently running agents per depth level. Per level
	// because a Lead waiting on its workers holds its own slot: one shared
	// pool would let four Leads wait forever on workers that cannot start.
	// Default 4; negative = unlimited.
	MaxParallel int `yaml:"max_parallel,omitempty"`
	// MaxMessages caps send_message + agent_post calls per turn — the guard
	// against two agents talking in circles. Default 64; negative = unlimited.
	MaxMessages int `yaml:"max_messages,omitempty"`
	// Threads keeps each sender→recipient conversation on disk
	// (.orchestra/agency/threads/) so a second send_message continues the
	// first instead of starting cold. Default true.
	Threads *bool `yaml:"threads,omitempty"`
	// RelayWorkOrders makes the runtime spawn the batch_workorders[] a Dept
	// Lead returns in task_result (spec §3.7, §5.6) instead of leaving the
	// Orchestrator to re-issue each one. Default true.
	RelayWorkOrders *bool `yaml:"relay_workorders,omitempty"`
}

// AgencyFlow is one parsed directional edge.
type AgencyFlow struct {
	From string
	To   string
}

// AgencyRootName is the address of the top-level agent of a turn.
const AgencyRootName = "orchestrator"

// agencyNameRe is the shape of an agent address: a role, a custom agent, a
// department type or a department instance (type@instance) — the same shape
// .orchestra/depts/{instance}.md accepts.
var agencyNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(@[a-z0-9][a-z0-9_-]*)?$`)

// ValidAgencyName reports whether name is a well-formed agent address.
func ValidAgencyName(name string) bool {
	return agencyNameRe.MatchString(name)
}

// AgencyType strips a department instance suffix: frontend@web → frontend.
func AgencyType(name string) string {
	if i := strings.Index(name, "@"); i > 0 {
		return name[:i]
	}
	return name
}

// ParseAgencyFlows parses flow lines into edges. Each line is a chain of
// names joined by ">"; every adjacent pair becomes one edge.
func ParseAgencyFlows(lines []string) ([]AgencyFlow, error) {
	var out []AgencyFlow
	for i, line := range lines {
		parts := strings.Split(line, ">")
		if len(parts) < 2 {
			return nil, fmt.Errorf("agency.flows[%d] %q: expected \"from > to\"", i, line)
		}
		names := make([]string, len(parts))
		for j, p := range parts {
			n := strings.ToLower(strings.TrimSpace(p))
			if n == "" {
				return nil, fmt.Errorf("agency.flows[%d] %q: empty agent name", i, line)
			}
			if n == "*" {
				if j != 0 {
					return nil, fmt.Errorf("agency.flows[%d] %q: \"*\" is only allowed on the left", i, line)
				}
			} else if !ValidAgencyName(n) {
				return nil, fmt.Errorf("agency.flows[%d] %q: invalid agent name %q", i, line, n)
			}
			names[j] = n
		}
		for j := 0; j+1 < len(names); j++ {
			from, to := names[j], names[j+1]
			if to == AgencyRootName {
				return nil, fmt.Errorf("agency.flows[%d] %q: %s is the root and cannot be a recipient — children report back with task_result, or leave a note with agent_post", i, line, AgencyRootName)
			}
			if from == to {
				return nil, fmt.Errorf("agency.flows[%d] %q: an agent cannot flow to itself", i, line)
			}
			out = append(out, AgencyFlow{From: from, To: to})
		}
	}
	return out, nil
}

// DefaultAgencyFlows are the spec's delegation edges (orchestra-routing §2.1:
// "Dept Lead L4 → Scout L2 (read-only) → Workers L3 / L1"; stage 0 "Product
// Lead + Market Scout"). Custom agents inherit the edges of their base.
func DefaultAgencyFlows() []AgencyFlow {
	return []AgencyFlow{
		{From: "product", To: "scout"},
		{From: "product", To: "explore"},
		{From: "documentation", To: "explore"},
		{From: "architecture", To: "explore"},
		{From: "architecture", To: "scout"},
		{From: "architecture", To: "worker"},
		{From: "architecture", To: "verifier"},
		{From: "debug", To: "explore"},
		{From: "debug", To: "worker"},
	}
}

// ResolvedEnabled reports whether the agency layer is active for a turn in
// the given top-level mode.
func (a AgencyConfig) ResolvedEnabled(mode string) bool {
	if a.Enabled != nil {
		return *a.Enabled
	}
	if len(a.Flows) > 0 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(mode), "orchestra")
}

// ResolvedDefaultFlows reports whether the spec's default edges apply (default true).
func (a AgencyConfig) ResolvedDefaultFlows() bool {
	return a.DefaultFlows == nil || *a.DefaultFlows
}

// ResolvedMaxDepth returns the nesting cap (default 2, clamped to 1..4).
func (a AgencyConfig) ResolvedMaxDepth() int {
	switch {
	case a.MaxDepth <= 0:
		return 2
	case a.MaxDepth > 4:
		return 4
	}
	return a.MaxDepth
}

// ResolvedMaxParallel returns the per-depth concurrency cap; 0 = unlimited.
func (a AgencyConfig) ResolvedMaxParallel() int {
	switch {
	case a.MaxParallel == 0:
		return 4
	case a.MaxParallel < 0:
		return 0
	}
	return a.MaxParallel
}

// ResolvedMaxMessages returns the per-turn message budget; 0 = unlimited.
func (a AgencyConfig) ResolvedMaxMessages() int {
	switch {
	case a.MaxMessages == 0:
		return 64
	case a.MaxMessages < 0:
		return 0
	}
	return a.MaxMessages
}

// ResolvedThreads reports whether conversations persist across turns (default true).
func (a AgencyConfig) ResolvedThreads() bool {
	return a.Threads == nil || *a.Threads
}

// ResolvedRelayWorkOrders reports whether the runtime relays a Lead's
// batch_workorders[] (default true).
func (a AgencyConfig) ResolvedRelayWorkOrders() bool {
	return a.RelayWorkOrders == nil || *a.RelayWorkOrders
}

// spawnableRoles are the built-in modes a parent may start as a child: every
// top-level mode that has a child protocol plus the child-only roles. build,
// plan, agent and orchestra are user-facing entry points, not subagents.
var spawnableRoles = map[string]bool{
	"explore": true, "ask": true, "debug": true, "architecture": true,
	"general": true, "worker": true, "verifier": true, "product": true,
	"documentation": true, "scout": true,
}

// IsSpawnableRole reports whether name is a built-in role that can run as a subagent.
func IsSpawnableRole(name string) bool {
	return spawnableRoles[strings.ToLower(strings.TrimSpace(name))]
}

// validateAgency checks agency: against the agents it may name.
func (c *ProjectConfig) validateAgency() error {
	a := c.Agency
	if a.MaxDepth > 4 {
		return fmt.Errorf("agency.max_depth must be between 1 and 4, got %d", a.MaxDepth)
	}
	flows, err := ParseAgencyFlows(a.Flows)
	if err != nil {
		return err
	}
	for _, f := range flows {
		if f.From == "*" {
			continue
		}
		if err := c.checkAgencyName(f.From, true); err != nil {
			return err
		}
		if err := c.checkAgencyName(f.To, false); err != nil {
			return err
		}
	}
	return nil
}

// checkAgencyName accepts a built-in role, a custom agent, the root (left side
// only) or a department address. Department types are open-ended (the spec
// freezes seven, projects add instances), so an unknown name is accepted as a
// department — but only when it is not a typo of a mode that cannot be a
// subagent, which would silently never match.
func (c *ProjectConfig) checkAgencyName(name string, left bool) error {
	if left && name == AgencyRootName {
		return nil
	}
	if IsSpawnableRole(name) || c.FindAgent(name) != nil {
		return nil
	}
	if _, reserved := builtInAgentModes[name]; reserved {
		return fmt.Errorf("agency.flows: %q is a top-level mode, not an agent that can be delegated to or messaged", name)
	}
	return nil
}

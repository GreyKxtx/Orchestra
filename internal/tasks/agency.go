package tasks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	tasktools "github.com/orchestra/orchestra/internal/tools/task"
	"github.com/orchestra/orchestra/llm"
)

// AgencySettings is the resolved agency: section for one turn. The zero value
// is the legacy runner — children cannot delegate or message, nothing talks
// sideways — except that MaxParallel and depends_on apply regardless.
type AgencySettings struct {
	Enabled         bool
	Flows           []config.AgencyFlow
	MaxDepth        int
	MaxParallel     int // per depth level; 0 = unlimited
	MaxMessages     int // per turn; 0 = unlimited
	Threads         bool
	RelayWorkOrders bool
}

// AgentProfile is a custom agent from agents: as a subagent.
type AgentProfile struct {
	Name         string
	Description  string
	Base         string   // built-in role it runs as
	SystemPrompt string   // role instructions, handed over as an <agent_role> block
	Tools        []string // short tool names; nil = the base role's tools
	Provider     string
	Model        string
	Tier         string
	MaxSteps     int
}

// AgencyFromConfig resolves the agency for a turn in the given top-level mode.
// Both the core and `orchestra apply` build a TaskRunner; this keeps them from
// drifting apart on what the agency means.
func AgencyFromConfig(cfg *config.ProjectConfig, mode string) (AgencySettings, []AgentProfile) {
	if cfg == nil {
		return AgencySettings{MaxParallel: 4}, nil
	}
	a := cfg.Agency
	out := AgencySettings{
		Enabled:         a.ResolvedEnabled(mode),
		MaxDepth:        a.ResolvedMaxDepth(),
		MaxParallel:     a.ResolvedMaxParallel(),
		MaxMessages:     a.ResolvedMaxMessages(),
		Threads:         a.ResolvedThreads(),
		RelayWorkOrders: a.ResolvedRelayWorkOrders(),
	}
	if a.ResolvedDefaultFlows() {
		out.Flows = append(out.Flows, config.DefaultAgencyFlows()...)
	}
	// Validated at Load; a parse error here means an in-memory config that
	// skipped validation, and the explicit edges are simply not added.
	if flows, err := config.ParseAgencyFlows(a.Flows); err == nil {
		out.Flows = append(out.Flows, flows...)
	}
	var profiles []AgentProfile
	for _, d := range cfg.Agents {
		if !config.ValidAgencyName(d.Name) {
			continue // a mode-string name nobody can address
		}
		var toolNames []string
		if d.Tools != nil {
			toolNames = append([]string(nil), d.Tools...)
		}
		profiles = append(profiles, AgentProfile{
			Name:         d.Name,
			Description:  strings.TrimSpace(d.Description),
			Base:         d.ResolvedBase(),
			SystemPrompt: strings.TrimSpace(d.SystemPrompt),
			Tools:        toolNames,
			Provider:     strings.TrimSpace(d.Provider),
			Model:        strings.TrimSpace(d.Model),
			Tier:         strings.TrimSpace(d.Tier),
			MaxSteps:     d.MaxSteps,
		})
	}
	return out, profiles
}

// builtInRoleCards describe the spawnable built-in roles for
// <available_agents>. Short on purpose: the Orchestrator pays for them on
// every step.
var builtInRoleCards = map[string]string{
	"explore":       "read-only code search; returns files, symbols, lines",
	"ask":           "read-only Q&A about the code",
	"scout":         "web research: competitors, market, prices — with sources",
	"architecture":  "Dept Lead: brief, spec, L2 playbook, WorkOrders (batch_workorders[])",
	"debug":         "root-cause a failure and fix it",
	"general":       "multi-step read+write execution",
	"worker":        "one atomic change from a WorkOrder JSON",
	"verifier":      "goal-backward read-only check of finished work",
	"product":       "Product Lead: PRD and user stories from an idea",
	"documentation": "Docs Lead: conventions.md, MANIFEST, docs/",
}

var builtInRoleOrder = []string{"explore", "scout", "ask", "product", "documentation", "architecture", "worker", "verifier", "debug", "general"}

// agentScope is where an agent sits in the agency: its address, the role it
// runs as, how deep it is and who is waiting on it.
type agentScope struct {
	address      string   // orchestrator | department instance | custom agent | role
	role         string   // built-in role (base of a custom agent)
	name         string   // subagent_type as spawned (custom agent name or role)
	dept         string   // department instance it works for ("" = none)
	depth        int      // root = 0
	chain        []string // addresses from the root down to this agent, inclusive
	taskID       string   // "" for the root
	parentTaskID string   // task of the agent that started this one
}

func rootScope() agentScope {
	return agentScope{
		address: config.AgencyRootName,
		role:    config.AgencyRootName,
		name:    config.AgencyRootName,
		chain:   []string{config.AgencyRootName},
	}
}

func (s agentScope) child(address, role, name, taskID string) agentScope {
	chain := append(append([]string(nil), s.chain...), address)
	return agentScope{address: address, role: role, name: name, depth: s.depth + 1, chain: chain, taskID: taskID, parentTaskID: s.taskID}
}

// inChain reports whether address is this agent or one of the agents waiting
// on it. Messaging one of them would deadlock: it is blocked on us.
func (s agentScope) inChain(address string) bool {
	for _, a := range s.chain {
		if a == address {
			return true
		}
	}
	return false
}

// spawnTarget is what a subagent_type (plus dept) resolves to.
type spawnTarget struct {
	role    string        // built-in role the child runs as
	name    string        // subagent_type as requested
	address string        // department instance, custom agent name or role
	profile *AgentProfile // custom agent, nil for a built-in role
}

func (r *TaskRunner) findProfile(name string) *AgentProfile {
	for i := range r.child.Agents {
		if r.child.Agents[i].Name == name {
			return &r.child.Agents[i]
		}
	}
	return nil
}

// fileChangingTools are the tools through which a child changes the
// workspace or starts one that does.
var fileChangingTools = map[string]bool{
	"write": true, "edit": true, "fs.delete": true, "fs.rename": true,
	"ast_rename": true, "lsp.rename": true, "bash": true,
	"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
	"git.worktree.add": true, "git.worktree.remove": true, "git.worktree.prune": true,
	"gh.pr.create": true, "skill_invoke": true, "task": true, "task_spawn": true,
}

// changesFiles reports whether a child of this target can change files: its
// role writes, or the tool list its custom agent was given does.
func (t spawnTarget) changesFiles() bool {
	if !agent.ReadOnlyRole(t.role) {
		return true
	}
	if t.profile == nil {
		return false
	}
	for _, n := range t.profile.Tools {
		if fileChangingTools[n] {
			return true
		}
	}
	return false
}

// resolveTarget maps a subagent_type (and optional dept) to the role the child
// runs as and the address it answers to. An unknown type keeps the legacy
// meaning (a mode name, gated as a writer by the phase guard).
func (r *TaskRunner) resolveTarget(subagentType, dept string) (spawnTarget, error) {
	name := strings.TrimSpace(subagentType)
	if name == "" {
		name = "explore"
	}
	t := spawnTarget{role: name, name: name, address: strings.ToLower(name)}
	if p := r.findProfile(name); p != nil && !config.IsSpawnableRole(name) {
		t.role = p.Base
		t.profile = p
	}
	if d := strings.ToLower(strings.TrimSpace(dept)); d != "" {
		if !config.ValidAgencyName(d) {
			return spawnTarget{}, fmt.Errorf("dept %q: expected a department instance like backend or frontend@web", dept)
		}
		t.address = d
	}
	// The address names inbox and thread files; a legacy free-form type
	// ("Refactor/Helper") keeps working as a mode but answers as "agent".
	if !config.ValidAgencyName(t.address) {
		t.address = "agent"
	}
	return t, nil
}

// flowAllows reports whether from may delegate to or converse with the target.
// The root is the hub and reaches everyone; below it only the edges count.
func (r *TaskRunner) flowAllows(from agentScope, t spawnTarget) bool {
	if from.depth == 0 {
		return true
	}
	for _, f := range r.child.Agency.Flows {
		if !flowMatchesFrom(f.From, from) {
			continue
		}
		if f.To == t.address || f.To == config.AgencyType(t.address) || f.To == t.name || f.To == t.role {
			return true
		}
	}
	return false
}

func flowMatchesFrom(from string, s agentScope) bool {
	return from == "*" || from == s.address || from == config.AgencyType(s.address) || from == s.name || from == s.role
}

// checkReach enforces flows and depth for one delegation or conversation.
func (r *TaskRunner) checkReach(from agentScope, t spawnTarget, verb string) error {
	if from.depth == 0 {
		return nil
	}
	if !r.child.Agency.Enabled {
		return fmt.Errorf("%s: subagents cannot %s — the agency is off for this turn (agency.enabled)", from.address, verb)
	}
	if from.depth+1 > r.child.Agency.MaxDepth {
		return fmt.Errorf("%s: cannot %s %s — depth %d is the limit (agency.max_depth=%d); report back with task_result and let your parent do it", from.address, verb, t.address, from.depth, r.child.Agency.MaxDepth)
	}
	if !r.flowAllows(from, t) {
		return fmt.Errorf("%s: no flow %s > %s in agency.flows; you may %s: %s", from.address, from.address, t.address, verb, strings.Join(r.reachableNames(from), ", "))
	}
	return nil
}

// reachableNames lists the flow targets of from, for refusal messages.
func (r *TaskRunner) reachableNames(from agentScope) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range r.child.Agency.Flows {
		if flowMatchesFrom(f.From, from) && !seen[f.To] {
			seen[f.To] = true
			out = append(out, f.To)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"(none)"}
	}
	return out
}

// agencyInfo is the agent-facing view of scope s.
//
// The root is the hub: it may delegate to every role and custom agent and
// talk to any department. Below it the flows decide. Messaging starts the
// recipient one level down, so an agent at the depth limit has neither.
func (r *TaskRunner) agencyInfo(s agentScope) agent.AgencyInfo {
	info := agent.AgencyInfo{Enabled: r.child.Agency.Enabled, Self: s.address, Depth: s.depth}
	if !info.Enabled {
		// Off, the root still delegates to custom agents by name (agents:
		// in a build-mode turn); only its subagent_type enum needs them.
		if s.depth == 0 && len(r.child.Agents) > 0 {
			for _, role := range builtInRoleOrder {
				info.Delegates = append(info.Delegates, agent.AgentCard{Name: role, Role: role, Description: builtInRoleCards[role], BuiltIn: true})
			}
			for _, p := range r.child.Agents {
				info.Delegates = append(info.Delegates, agent.AgentCard{Name: p.Name, Role: p.Base, Description: p.Description})
			}
		}
		return info
	}
	if s.depth > 0 && s.depth+1 > r.child.Agency.MaxDepth {
		return info
	}
	for _, role := range builtInRoleOrder {
		t := spawnTarget{role: role, name: role, address: role}
		if !r.flowAllows(s, t) {
			continue
		}
		card := agent.AgentCard{Name: role, Role: role, Description: builtInRoleCards[role], BuiltIn: true}
		info.Delegates = append(info.Delegates, card)
		if s.depth == 0 && isLeadRole(role) {
			info.Contacts = append(info.Contacts, card)
		}
	}
	for i := range r.child.Agents {
		p := &r.child.Agents[i]
		t := spawnTarget{role: p.Base, name: p.Name, address: p.Name, profile: p}
		if !r.flowAllows(s, t) {
			continue
		}
		card := agent.AgentCard{Name: p.Name, Role: p.Base, Description: p.Description}
		info.Delegates = append(info.Delegates, card)
		if p.Base != "worker" {
			info.Contacts = append(info.Contacts, card)
		}
	}
	if s.depth == 0 {
		info.Contacts = appendCard(info.Contacts, agent.AgentCard{Name: "<department>", Role: "architecture", Description: "any department by name (backend, frontend@web): its Lead answers and keeps the conversation"})
		return info
	}
	// Department edges (frontend > backend) name no role or custom agent;
	// they are conversation targets, answered by the department's Lead.
	for _, f := range r.child.Agency.Flows {
		if !flowMatchesFrom(f.From, s) || config.IsSpawnableRole(f.To) || r.findProfile(f.To) != nil {
			continue
		}
		info.Contacts = appendCard(info.Contacts, agent.AgentCard{Name: f.To, Role: "architecture", Description: "department (its Lead answers)"})
	}
	return info
}

func appendCard(cards []agent.AgentCard, c agent.AgentCard) []agent.AgentCard {
	for _, x := range cards {
		if x.Name == c.Name {
			return cards
		}
	}
	return append(cards, c)
}

// isLeadRole reports whether role is a lead that holds a conversation well —
// the roles send_message defaults a department to.
func isLeadRole(role string) bool {
	switch role {
	case "architecture", "product", "documentation", "debug":
		return true
	}
	return false
}

// delegationTools are the spawn tools a child gets when the flows let it
// delegate. task_cancel stays with the root: a Lead that needs to cancel its
// own worker has misjudged the WorkOrder and should report that instead.
func delegationTools() []llm.ToolDef {
	out := []llm.ToolDef{tasktools.ToolTask(), tasktools.ToolTaskSpawn(), tasktools.ToolTaskWait()}
	for i := range out {
		out[i].Mutating = true
	}
	return out
}

// withoutDelegationTools drops spawn tools a custom agent listed itself: the
// flows decide whether a child delegates, not its tool list.
func withoutDelegationTools(defs []llm.ToolDef) []llm.ToolDef {
	out := defs[:0:0]
	for _, d := range defs {
		switch d.Function.Name {
		case "task", "task_spawn", "task_wait", "task_cancel", "send_message", "agent_post", "task_board":
			continue
		}
		out = append(out, d)
	}
	return out
}

// childToolsForTarget is the tool surface of a child: its role's list (or the
// custom agent's own list), never the repo-mutating git tools, always
// task_result, and the spawn tools only when the agency lets it delegate.
func (r *TaskRunner) childToolsForTarget(t spawnTarget, childScope agentScope) []llm.ToolDef {
	var defs []llm.ToolDef
	if t.profile != nil && t.profile.Tools != nil {
		resolved, err := tools.ResolveToolNamesWithPolicy(t.profile.Tools, r.child.Caps)
		if err == nil {
			defs = resolved
		}
	}
	if defs == nil {
		defs = childToolsForSubagent(t.role, r.child.Caps)
	}
	defs = withoutDelegationTools(tools.StripRepoMutatingTools(defs))
	if len(r.agencyInfo(childScope).Delegates) > 0 {
		defs = append(defs, delegationTools()...)
	}
	return ensureTaskResult(defs)
}

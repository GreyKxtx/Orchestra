// Package toolspec says what each built-in tool is: whether it may run in a
// parallel batch, which consent it needs, whether the Orchestra Lead is given
// it, and who handles it.
//
// These facts used to live in six places that had to agree —
// tools.parallelSafeTools, tools.mutatingTools, tools.orchestraLeadToolNames,
// tools.isExecGated / isWebGated / isBrowserGated, agent.isWebTool,
// agent.isAgentInProcessTool and config.validAgentToolNames — and a test kept
// two of them in step "to avoid an import cycle". They did not all agree:
// isExecGated named bash_output and bash_kill, which are no tools, so a custom
// agent listing bash.output was offered it without exec consent; and the
// read-only gh queries and the git mutators outside commit/branch/checkout/push
// were listed only with exec consent but resolved without it.
//
// The table is data with no imports, so config can validate names against it
// and the tools, agent and roles packages can all read it.
package toolspec

// Group is the set a tool is offered with, and so the consent it needs.
type Group int

const (
	// GroupBase tools are offered by name in a mode's list.
	GroupBase Group = iota
	// GroupExec needs exec consent: bash and the read-only gh queries.
	GroupExec
	// GroupRepoMutating rewrites git state or publishes: commit, branch,
	// checkout, push, worktrees, PR creation. Needs exec consent, is offered
	// only to top-level modes (children share the working tree) and only on
	// static consent (these calls do not ask the user).
	GroupRepoMutating
	// GroupWeb reaches the network on the model's behalf and needs web consent.
	GroupWeb
	// GroupBrowser drives the Playwright browser and needs browser consent.
	GroupBrowser
	// GroupSubtasks delegates work to child agents.
	GroupSubtasks
)

// Offer says how a tool reaches a tool list.
type Offer int

const (
	// ByName is a static definition any mode list or agents: entry may name.
	ByName Offer = iota
	// LeadOnly is a static definition whose handler refuses everyone but the
	// Orchestra Lead, so an agents: entry may not name it.
	LeadOnly
	// Runtime tools are built by the agent for the turn (skill_invoke carries
	// the skill names; the agency tools exist only when the agency is on).
	Runtime
)

// Spec is one built-in tool.
type Spec struct {
	Name string
	// Parallel tools only read, and may run together in one batch. Every
	// other built-in tool runs alone and in order.
	Parallel bool
	Group    Group
	Offer    Offer
	// Lead tools are on the Orchestra Lead's allowlist.
	Lead bool
	// InProcess tools are handled by the agent (session state, delegation,
	// questions) rather than by tools.Runner.
	InProcess bool
	// Untrusted tools return third-party text — a web page, a PR, what the
	// browser shows — that may carry instructions. The agent hands their
	// results to the model marked as data, and the turn that read one is
	// tainted (SEC-8).
	Untrusted bool
	// ActsForUser tools act with the user's authority beyond the staged
	// workspace: they run commands or publish. In a tainted turn each call
	// needs the user's yes, blanket consent or not (SEC-8).
	ActsForUser bool
}

// Mutating reports whether the tool must run serially. Every built-in tool is
// one or the other; tools from outside the table (MCP) are neither and keep
// the conservative default.
func (s Spec) Mutating() bool { return !s.Parallel }

// Consent is the capability a tool needs before it may be offered.
func (s Spec) Consent() Group {
	switch s.Group {
	case GroupRepoMutating:
		return GroupExec
	case GroupExec, GroupWeb, GroupBrowser:
		return s.Group
	}
	return GroupBase
}

// table lists every built-in tool. Within a group the order is the order the
// group is appended to a tool list, which is part of the prompt the model
// sees; keep it stable.
var table = []Spec{
	// Files.
	{Name: "ls", Parallel: true},
	{Name: "read", Parallel: true, Lead: true},
	{Name: "glob", Parallel: true},
	{Name: "write", Lead: true},
	{Name: "edit"},
	{Name: "fs.delete"},
	{Name: "fs.rename"},
	{Name: "ast_rename"},
	{Name: "diff.preview", Parallel: true},

	// Search and navigation.
	{Name: "grep", Parallel: true, Lead: true},
	{Name: "symbols", Parallel: true},
	{Name: "explore", Parallel: true, Lead: true},
	{Name: "repo_map", Parallel: true, Lead: true},
	{Name: "semantic_search", Parallel: true},
	{Name: "runtime_query", Parallel: true},

	// LSP.
	{Name: "lsp.definition", Parallel: true},
	{Name: "lsp.references", Parallel: true},
	{Name: "lsp.hover", Parallel: true},
	{Name: "lsp.diagnostics", Parallel: true},
	{Name: "lsp.rename"},

	// Session state and memory.
	{Name: "todowrite", InProcess: true},
	{Name: "todoread", InProcess: true},
	{Name: "memory_write"},
	{Name: "memory_read", Lead: true},
	{Name: "memory_search", Lead: true},
	{Name: "lesson_promote", Lead: true, InProcess: true},
	{Name: "playbook_promote", Lead: true, InProcess: true},
	{Name: "update_working_state", Offer: LeadOnly, Lead: true, InProcess: true},
	{Name: "contract_freeze", Offer: LeadOnly, Lead: true, InProcess: true},
	{Name: "question", Lead: true, InProcess: true},
	{Name: "plan_exit", InProcess: true},

	// Read-only git.
	{Name: "git.status", Parallel: true},
	{Name: "git.log", Parallel: true},
	{Name: "git.diff", Parallel: true},
	{Name: "git.worktree.list", Parallel: true},

	// Exec.
	{Name: "bash", Group: GroupExec, ActsForUser: true},
	{Name: "bash.output", Group: GroupExec},
	{Name: "bash.kill", Group: GroupExec},
	{Name: "gh.pr.list", Parallel: true, Group: GroupExec, Untrusted: true},
	{Name: "gh.pr.view", Parallel: true, Group: GroupExec, Untrusted: true},
	{Name: "gh.issue.list", Parallel: true, Group: GroupExec, Untrusted: true},
	{Name: "gh.issue.view", Parallel: true, Group: GroupExec, Untrusted: true},

	// History-mutating git and publishing.
	{Name: "git.commit", Group: GroupRepoMutating},
	{Name: "git.branch", Group: GroupRepoMutating},
	{Name: "git.checkout", Group: GroupRepoMutating},
	{Name: "git.push", Group: GroupRepoMutating, ActsForUser: true},
	{Name: "git.worktree.add", Group: GroupRepoMutating},
	{Name: "git.worktree.remove", Group: GroupRepoMutating},
	{Name: "git.worktree.prune", Group: GroupRepoMutating},
	{Name: "gh.pr.create", Group: GroupRepoMutating, ActsForUser: true},

	// Web.
	{Name: "webfetch", Parallel: true, Group: GroupWeb, Untrusted: true},
	{Name: "websearch", Parallel: true, Group: GroupWeb, Untrusted: true},

	// Browser.
	{Name: "browser.navigate", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.snapshot", Parallel: true, Group: GroupBrowser, Untrusted: true},
	{Name: "browser.screenshot", Parallel: true, Group: GroupBrowser, Untrusted: true},
	{Name: "browser.click", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.type", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.fill", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.select", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.eval", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.wait", Group: GroupBrowser, Untrusted: true},
	{Name: "browser.close", Group: GroupBrowser, Untrusted: true},

	// Delegation.
	{Name: "task", Group: GroupSubtasks, Lead: true, InProcess: true},
	{Name: "task_spawn", Group: GroupSubtasks, Lead: true, InProcess: true},
	{Name: "task_wait", Group: GroupSubtasks, Lead: true, InProcess: true},
	{Name: "task_cancel", Group: GroupSubtasks, Lead: true, InProcess: true},
	{Name: "task_result", InProcess: true},
	{Name: "skill_invoke", Offer: Runtime, InProcess: true},

	// Agency: present only when the agency is on for the turn.
	{Name: "send_message", Offer: Runtime, Lead: true, InProcess: true},
	{Name: "agent_post", Offer: Runtime, Lead: true, InProcess: true},
	{Name: "task_board", Offer: Runtime, Lead: true, InProcess: true},
}

var byName = func() map[string]Spec {
	m := make(map[string]Spec, len(table))
	for _, s := range table {
		if _, dup := m[s.Name]; dup {
			panic("toolspec: duplicate tool " + s.Name)
		}
		m[s.Name] = s
	}
	return m
}()

// Lookup returns the tool's spec, and false for a name that is no built-in
// tool (an MCP tool, a typo).
func Lookup(name string) (Spec, bool) {
	s, ok := byName[name]
	return s, ok
}

// All returns every built-in tool in table order.
func All() []Spec {
	return append([]Spec(nil), table...)
}

// InGroup returns the names in g, in the order they are offered.
func InGroup(g Group) []string {
	var out []string
	for _, s := range table {
		if s.Group == g {
			out = append(out, s.Name)
		}
	}
	return out
}

// Nameable reports whether an agents: entry or a skill's tools: list may name
// the tool.
func Nameable(name string) bool {
	s, ok := byName[name]
	return ok && s.Offer == ByName
}

// NameableNames returns every name Nameable accepts, in table order.
func NameableNames() []string {
	var out []string
	for _, s := range table {
		if s.Offer == ByName {
			out = append(out, s.Name)
		}
	}
	return out
}

// IsWeb reports whether the tool reaches the network on the model's behalf.
func IsWeb(name string) bool {
	s, ok := byName[name]
	return ok && s.Group == GroupWeb
}

// IsInProcess reports whether the agent, not tools.Runner, handles the tool.
func IsInProcess(name string) bool {
	s, ok := byName[name]
	return ok && s.InProcess
}

// IsLead reports whether the tool is on the Orchestra Lead's allowlist.
func IsLead(name string) bool {
	s, ok := byName[name]
	return ok && s.Lead
}

// ResultUntrusted reports whether a tool's result is third-party text: a
// built-in marked Untrusted, or any MCP tool — its server is not ours, and
// neither is what it returns.
func ResultUntrusted(name string) bool {
	if len(name) > len("mcp:") && name[:len("mcp:")] == "mcp:" {
		return true
	}
	s, ok := byName[name]
	return ok && s.Untrusted
}

// ActsForUser reports whether a tool acts with the user's authority beyond
// the staged workspace (Spec.ActsForUser).
func ActsForUser(name string) bool {
	s, ok := byName[name]
	return ok && s.ActsForUser
}

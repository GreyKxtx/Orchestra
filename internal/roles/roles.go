// Package roles is the registry of agent modes. One Spec says what a mode is
// called, who may start it, which tools it is offered, where it may write,
// whom it may start and which model its children run on.
//
// A mode used to be spread over 6–7 files: 17 constants in agent, the same 17
// names in config, a switch and a builder per mode in the tool registry, write
// guards in agent and plan, role cards, lead and spawnable lists in tasks and
// config, and the phase gate's list in orchestrastate — each a list of names
// that had to agree with the others. A new mode is now a Spec here and a
// prompt file in internal/prompt/files.
//
// The package is data with no imports beyond toolspec, so config validates
// names against it and every other layer reads it.
package roles

import "github.com/orchestra/orchestra/internal/toolspec"

// Kind is how a mode may be started.
type Kind int

const (
	// TopLevel can be requested by the user (CLI --mode, RPC agent.run) and
	// can also be spawned as a subagent when Spawnable.
	TopLevel Kind = iota
	// ChildOnly is a subagent role with its own protocol (task_result,
	// WorkOrder, scoped writes). Starting one top-level skips the contract that
	// gives it its input, so the CLI and RPC refuse it.
	ChildOnly
	// Internal is driven by the runtime itself (history compaction, title,
	// summary) and never selected by a user.
	Internal
)

// Write is where a mode may change files.
type Write int

const (
	// WriteAny changes any file in the project.
	WriteAny Write = iota
	// WriteNone only reads: write, edit, delete, rename and skills are refused.
	WriteNone
	// WritePlan writes the plan file, .orchestra/plans/*.md, state.md and the
	// department scratchpads (plan.IsWritablePath).
	WritePlan
	// WriteOrchestraLead is the Orchestra Lead's surface
	// (plan.IsOrchestraLeadWritablePath).
	WriteOrchestraLead
	// WriteDeptLead is the Dept Lead's: plans, its L2 playbook and specs
	// (plan.IsDeptLeadWritablePath).
	WriteDeptLead
	// WriteProduct is .orchestra/product/ only.
	WriteProduct
	// WriteDocs is conventions.md, .orchestra/docs/ and docs/ minus the
	// operations runbooks.
	WriteDocs
	// WriteTargets is the WorkOrder's target_files.
	WriteTargets
)

// Tier is which model a child of this role runs on when the spawn names none.
type Tier int

const (
	// TierParent runs on the spawner's client.
	TierParent Tier = iota
	// TierWorker takes a worker band from orchestra.tiers (the spawn's tier).
	TierWorker
	// TierLead takes the "lead" band unless the spawn names a tier.
	TierLead
)

// Tools is a mode's tool list: tools named outright, then groups appended when
// the turn has the consent or the runtime piece they need, in this order.
type Tools struct {
	// Names are always offered, in this order.
	Names []string
	// Caps appends the exec, web and browser groups, each with its consent.
	Caps bool
	// RepoMutating appends the git mutators with exec consent (top level only;
	// children have them stripped).
	RepoMutating bool
	// Subtasks appends task, task_spawn, task_wait and task_cancel when the
	// turn has a subtask runner.
	Subtasks bool
	// Question appends question when the turn can ask the user.
	Question bool
	// Lead narrows the list to the Orchestra Lead allowlist with compacted
	// descriptions.
	Lead bool
}

// Spec is one agent mode.
type Spec struct {
	Name string
	Kind Kind
	// Prompt is the stem of the system prompt file in internal/prompt/files.
	Prompt string
	// Summary is the one-line card <available_agents> shows for a spawnable
	// role. Short on purpose: the Orchestrator pays for it on every step.
	Summary string
	Tools   Tools
	Write   Write
	// NoExec refuses bash even with consent.
	NoExec bool
	// Spawnable roles may run as a subagent.
	Spawnable bool
	// CanSpawn are the roles this one starts under agency.default_flows.
	CanSpawn []string
	// Lead roles hold a conversation for a department: send_message to a
	// department is answered by one of them.
	Lead bool
	// Tier is the model a child of this role runs on.
	Tier Tier
	// ReadOnlyChildren: at the top level this mode promised the user not to
	// change files, so it may only delegate to roles that leave them alone.
	ReadOnlyChildren bool
	// ExploreFirst refuses write and edit until the agent has looked at the
	// code (read, grep, explore, …).
	ExploreFirst bool
}

// ReadOnly reports whether the mode leaves files alone.
func (s Spec) ReadOnly() bool { return s.Write == WriteNone }

// WritesCode reports whether the mode can change production files: anywhere,
// or a WorkOrder's targets. Such a child is phase-gated, and such a mode may
// be offered MCP tools that change things. The document-writing leads (plan,
// orchestra, architecture, product, documentation) are not.
func (s Spec) WritesCode() bool { return s.Write == WriteAny || s.Write == WriteTargets }

// Names shared by several modes' lists.
var (
	lspRead = []string{"lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics"}
	gitRead = []string{"git.status", "git.log", "git.diff", "git.worktree.list"}
)

func names(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func list(n ...string) []string { return n }

var buildTools = Tools{
	Names: names(
		list("ls", "read", "glob", "write", "edit", "fs.delete", "fs.rename",
			"grep", "symbols", "explore", "repo_map", "diff.preview", "runtime_query",
			"todowrite", "todoread", "memory_write", "memory_read", "memory_search"),
		lspRead, list("lsp.rename"), gitRead),
	Caps: true, RepoMutating: true, Subtasks: true, Question: true,
}

// registry is every built-in mode. The order of the spawnable roles is the
// order of their cards in <available_agents>.
var registry = []Spec{
	{
		Name: "build", Kind: TopLevel, Prompt: "build",
		Tools: buildTools, Write: WriteAny,
	},
	{
		Name: "plan", Kind: TopLevel, Prompt: "plan",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "write",
					"grep", "symbols", "explore", "repo_map", "diff.preview", "runtime_query",
					"todowrite", "todoread", "plan_exit"),
				lspRead),
			Subtasks: true, Question: true,
		},
		Write: WritePlan, ReadOnlyChildren: true,
	},
	{
		// Resolved to build, plan, explore or ask before the run; if still
		// seen, it is build.
		Name: "agent", Kind: TopLevel, Prompt: "build",
		Tools: buildTools, Write: WriteAny,
	},
	{
		Name: "orchestra", Kind: TopLevel, Prompt: "orchestra",
		Tools: Tools{
			Names: list("read", "grep", "explore", "repo_map", "write",
				"memory_read", "memory_search", "lesson_promote", "playbook_promote",
				"update_working_state", "contract_freeze"),
			Subtasks: true, Question: true, Lead: true,
		},
		Write: WriteOrchestraLead, ExploreFirst: true,
	},
	{
		Name: "explore", Kind: TopLevel, Prompt: "explore",
		Summary: "read-only code search; returns files, symbols, lines",
		Tools: Tools{Names: names(
			list("ls", "read", "glob", "grep", "symbols", "explore", "repo_map"),
			lspRead)},
		Write: WriteNone, Spawnable: true,
	},
	{
		// Web research on competitors and the market, plus repository reads
		// for a brownfield product. Web tools are always listed; the runtime
		// web consent still decides whether a call goes out.
		Name: "scout", Kind: ChildOnly, Prompt: "scout",
		Summary: "web research: competitors, market, prices — with sources",
		Tools: Tools{Names: list("ls", "read", "glob", "grep", "repo_map",
			"task_result", "webfetch", "websearch")},
		Write: WriteNone, Spawnable: true,
	},
	{
		// repo_map is the cheapest answer to "what is this project?" — one
		// call, a per-file outline under a byte budget.
		Name: "ask", Kind: TopLevel, Prompt: "ask",
		Summary: "read-only Q&A about the code",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "grep", "symbols", "explore", "repo_map"),
				lspRead),
			Question: true,
		},
		Write: WriteNone, NoExec: true, Spawnable: true, ReadOnlyChildren: true,
	},
	{
		// Product Lead (spec §3.2): repository reads for brownfield context,
		// writes in .orchestra/product/, web for market research. As a child
		// it may send scouts out (the agency's product > scout edge).
		Name: "product", Kind: ChildOnly, Prompt: "product",
		Summary: "Product Lead: PRD and user stories from an idea",
		Tools: Tools{
			Names: list("ls", "read", "glob", "write", "edit", "grep", "repo_map",
				"todowrite", "todoread", "task_result", "webfetch", "websearch"),
			Subtasks: true, Question: true,
		},
		Write: WriteProduct, Spawnable: true, CanSpawn: list("scout", "explore"),
		Lead: true, Tier: TierLead,
	},
	{
		// Docs Lead (spec §2.3.2): full repository reads for stack detection
		// and brownfield docs. No web, no exec, no spawn tools of its own.
		Name: "documentation", Kind: ChildOnly, Prompt: "documentation",
		Summary: "Docs Lead: conventions.md, MANIFEST, docs/",
		Tools: Tools{
			Names: list("ls", "read", "glob", "write", "edit",
				"grep", "symbols", "explore", "repo_map",
				"todowrite", "todoread", "task_result",
				"git.status", "git.log", "git.diff"),
			Question: true,
		},
		Write: WriteDocs, Spawnable: true, CanSpawn: list("explore"),
		Lead: true, Tier: TierLead,
	},
	{
		// Design only: plan documents, the L2 playbook and specs, research,
		// and delegation — a Dept Lead as a child hands out WorkOrders.
		Name: "architecture", Kind: TopLevel, Prompt: "architecture",
		Summary: "Dept Lead: brief, spec, L2 playbook, WorkOrders (batch_workorders[])",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "write",
					"grep", "symbols", "explore", "repo_map", "diff.preview", "runtime_query",
					"todowrite", "todoread", "plan_exit",
					"lesson_promote", "playbook_promote",
					"memory_write", "memory_read", "memory_search"),
				lspRead, gitRead),
			Subtasks: true, Question: true,
		},
		Write: WriteDeptLead, Spawnable: true,
		CanSpawn: list("explore", "scout", "worker", "verifier"),
		Lead:     true, ReadOnlyChildren: true, ExploreFirst: true,
	},
	{
		// The atomic implementer: one WorkOrder, edits inside its targets.
		Name: "worker", Kind: ChildOnly, Prompt: "worker",
		Summary: "one atomic change from a WorkOrder JSON",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "write", "edit",
					"grep", "symbols", "explore", "repo_map", "diff.preview", "task_result"),
				lspRead),
			Caps: true,
		},
		Write: WriteTargets, Spawnable: true, Tier: TierWorker, ExploreFirst: true,
	},
	{
		// Goal-backward verification: reads, diagnostics, and bash to run the
		// checks.
		Name: "verifier", Kind: ChildOnly, Prompt: "verifier",
		Summary: "goal-backward read-only check of finished work",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "grep", "symbols", "explore", "repo_map", "diff.preview"),
				lspRead, list("git.status", "git.diff")),
			Caps: true,
		},
		Write: WriteNone, Spawnable: true,
	},
	{
		Name: "debug", Kind: TopLevel, Prompt: "debug",
		Summary: "root-cause a failure and fix it",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "write", "edit",
					"grep", "symbols", "explore", "repo_map", "diff.preview", "runtime_query",
					"todowrite", "todoread"),
				lspRead, list("lsp.rename"), gitRead),
			Caps: true, RepoMutating: true, Subtasks: true, Question: true,
		},
		Write: WriteAny, Spawnable: true, CanSpawn: list("explore", "worker"), Lead: true,
	},
	{
		// Multi-step execution subagent: full read and write, reports through
		// task_result. No todowrite: it tracks progress itself.
		Name: "general", Kind: TopLevel, Prompt: "general",
		Summary: "multi-step read+write execution",
		Tools: Tools{
			Names: names(
				list("ls", "read", "glob", "write", "edit", "fs.delete", "fs.rename",
					"grep", "symbols", "explore", "repo_map", "diff.preview", "runtime_query",
					"todoread", "memory_write", "memory_read", "memory_search", "task_result"),
				lspRead, list("lsp.rename"), gitRead),
			Caps: true, RepoMutating: true, Subtasks: true,
		},
		Write: WriteAny, Spawnable: true,
	},
	{Name: "compaction", Kind: Internal, Prompt: "compaction", Write: WriteNone},
	{Name: "title", Kind: Internal, Prompt: "title", Write: WriteNone},
	{Name: "summary", Kind: Internal, Prompt: "summary", Write: WriteNone},
}

var byName = func() map[string]Spec {
	m := make(map[string]Spec, len(registry))
	for _, s := range registry {
		if _, dup := m[s.Name]; dup {
			panic("roles: duplicate mode " + s.Name)
		}
		for _, n := range s.Tools.Names {
			if _, ok := toolspec.Lookup(n); !ok {
				panic("roles: mode " + s.Name + " lists unknown tool " + n)
			}
		}
		m[s.Name] = s
	}
	return m
}()

// Lookup returns the mode's spec, and false for a name that is no built-in
// mode (a custom agent, a typo).
func Lookup(name string) (Spec, bool) {
	s, ok := byName[name]
	return s, ok
}

// All returns every built-in mode in registry order.
func All() []Spec {
	return append([]Spec(nil), registry...)
}

// Spawnable returns the spawnable roles in card order.
func Spawnable() []Spec {
	var out []Spec
	for _, s := range registry {
		if s.Spawnable {
			out = append(out, s)
		}
	}
	return out
}

// IsSpawnable reports whether name is a built-in role that can run as a
// subagent.
func IsSpawnable(name string) bool {
	s, ok := byName[name]
	return ok && s.Spawnable
}

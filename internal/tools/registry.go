package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/tools/git"
	"github.com/orchestra/orchestra/internal/tools/nav"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools/task"
	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/internal/tools/web"
	"github.com/orchestra/orchestra/internal/toolspec"
	"github.com/orchestra/orchestra/llm"
)

// Capabilities is the bundle of capability flags every tool-listing
// function needs. M5 in architecture audit: previously these were three
// independent bool parameters threaded through 7+ signatures, so a 4th
// capability would have forced every call site to grow. Pass-by-value
// is cheap (a 3-bool struct) and the field names make the intent
// readable at call sites — `Capabilities{Exec: true}` is clearer than
// `(true, false, false)`.
type Capabilities struct {
	Exec    bool
	Web     bool
	Browser bool
}

// appendExecTools adds command execution plus read-only GitHub queries.
//
// History-mutating git and PR creation are NOT here — see
// appendRepoMutatingTools. One flag used to advertise both, which put
// git.commit / git.push / gh.pr.create in the schema of modes whose own
// prompt says read-only.
func appendExecTools(out []llm.ToolDef) []llm.ToolDef {
	return appendGroup(out, toolspec.GroupExec)
}

// appendRepoMutatingTools adds the tools that rewrite git state or publish to
// the remote: commit, branch, checkout, push, worktree management, PR creation.
//
// Only user-facing top-level modes get these. A subagent must not have them:
// children share the parent's Runner and therefore its working tree, so a
// git.checkout in one child switches the branch under every sibling and the
// parent — and a worker's job ends at a patch, with committing and publishing
// left to the user.
//
// Gated on the same exec consent, since bash could run these commands anyway;
// the point is what the model is *told* it may do, which is what it acts on.
func appendRepoMutatingTools(out []llm.ToolDef, caps Capabilities) []llm.ToolDef {
	if !caps.Exec {
		return out
	}
	return appendGroup(out, toolspec.GroupRepoMutating)
}

// appendWebTools adds web fetch + search tools to out.
func appendWebTools(out []llm.ToolDef) []llm.ToolDef {
	return appendGroup(out, toolspec.GroupWeb)
}

// appendBrowserTools adds the Playwright-MCP browser tools to out.
func appendBrowserTools(out []llm.ToolDef) []llm.ToolDef {
	return appendGroup(out, toolspec.GroupBrowser)
}

// appendSubtaskTools adds unified task + async spawn/wait/cancel to out.
func appendSubtaskTools(out []llm.ToolDef) []llm.ToolDef {
	return appendGroup(out, toolspec.GroupSubtasks)
}

// appendGroup appends the group's tools in toolspec order.
func appendGroup(out []llm.ToolDef, g toolspec.Group) []llm.ToolDef {
	return append(out, defs(toolspec.InGroup(g)...)...)
}

// appendCapabilityTools layers exec / web / browser conditionally — the
// flag pattern repeated across ListTools, listToolsBuild and
// listToolsGeneral collapses to one call. S3 in audit ledger; M5 swapped
// the three bool args for a Capabilities struct.
func appendCapabilityTools(out []llm.ToolDef, caps Capabilities) []llm.ToolDef {
	if caps.Exec {
		out = appendExecTools(out)
	}
	if caps.Web {
		out = appendWebTools(out)
	}
	if caps.Browser {
		out = appendBrowserTools(out)
	}
	return out
}

// ListTools returns the MAXIMAL set of tools a top-level agent could
// use — used by agent.computeToolDefs when no specific mode applies and
// no subtasks are configured, and by the parallel-flags safety test as
// the surface to enumerate.
//
// Differs from listToolsBuild (the build-mode set) by including
// ast_rename and repo_map. plan_enter is not advertised on any surface
// (legacy stub only — enter plan via --mode plan / RPC mode).
//
// Other ListTools* surfaces in this file are intentionally distinct:
//   - ListToolsWithSubtasks → ListTools + task_spawn/wait/cancel
//   - ListToolsForMode      → mode-aware dispatch (build/plan/explore/general)
//   - ListToolsForChild     → restricted read-only set + task_result
//   - ListToolsForInvestigator → child + runtime_query
//
// MCP / Custom / Extra / Skill tools are layered on top by
// agent.computeToolDefs, NOT here. M5 in architecture audit collapsed
// the parameter list from three bools to a Capabilities struct.
func ListTools(caps Capabilities) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(),
		fs.ToolFSRead(),
		fs.ToolFSGlob(),
		fs.ToolFSWrite(),
		fs.ToolFSEdit(),
		fs.ToolFSDelete(),
		fs.ToolFSRename(),
		fs.ToolASTRename(),
		fs.ToolSearchText(),
		nav.ToolCodeSymbols(),
		nav.ToolExploreCodebase(),
		nav.ToolRepoMap(),
		fs.ToolDiffPreview(),
		session.ToolRuntimeQuery(),
		session.ToolTodoWrite(),
		session.ToolTodoRead(),
		session.ToolMemoryWrite(),
		session.ToolMemoryRead(),
		session.ToolMemorySearch(),
		toolslsp.ToolLSPDefinition(),
		toolslsp.ToolLSPReferences(),
		toolslsp.ToolLSPHover(),
		toolslsp.ToolLSPDiagnostics(),
		toolslsp.ToolLSPRename(),
		git.ToolGitStatus(),
		git.ToolGitLog(),
		git.ToolGitDiff(),
		git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
	out = appendRepoMutatingTools(out, caps)
	return applyParallelFlags(out)
}

// StripRepoMutatingTools removes the tools appendRepoMutatingTools adds —
// commit, branch, checkout, push, worktree management, PR creation.
//
// A child agent shares the parent's Runner and therefore the parent's working
// tree, so a git.checkout in one child switches the branch under every sibling
// and the parent. Four of the nine subagent types get their tools from
// purpose-built child lists that never had these; debug and general reuse the
// top-level mode lists, which do, and were handed the whole set.
func StripRepoMutatingTools(defs []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(defs))
	for _, d := range defs {
		if s, ok := toolspec.Lookup(d.Function.Name); ok && s.Group == toolspec.GroupRepoMutating {
			continue
		}
		out = append(out, d)
	}
	return out
}

// applyParallelFlags marks each built-in tool ParallelSafe or Mutating from
// toolspec. A tool outside the table (MCP, plugins) is neither and keeps the
// conservative default: serial, not counted as a mutation.
func applyParallelFlags(defs []llm.ToolDef) []llm.ToolDef {
	for i := range defs {
		if s, ok := toolspec.Lookup(defs[i].Function.Name); ok {
			defs[i].ParallelSafe = s.Parallel
			defs[i].Mutating = s.Mutating()
		}
	}
	return defs
}

// H2 in architecture audit: ListToolsWithMCP and ListToolsWithSubtasks
// AndMCP were dead code (no callers in production or tests beyond their
// own definitions). MCP composition now happens by appending mcpDefs in
// the agent layer (agent.computeToolDefs) after one of the surviving
// ListTools* functions returns the base set. Removed in this audit.

// ListToolsWithMCP and ListToolsWithSubtasks AndMCP were dead code — see comment above.

// ToolNames returns tool function names for prompt/debug usage.
func ToolNames(defs []llm.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Function.Name)
	}
	sort.Strings(out)
	return out
}

// ListToolsWithSubtasks returns tools including task.spawn/wait/cancel for parent agents.
func ListToolsWithSubtasks(caps Capabilities) []llm.ToolDef {
	out := ListTools(caps)
	out = appendSubtaskTools(out)
	return applyParallelFlags(out)
}

// ListToolsForChild returns a restricted read-only tool set for child agents plus task.result.
// Child agents cannot write files, run commands, or spawn further subtasks.
func ListToolsForChild() []llm.ToolDef {
	return applyParallelFlags([]llm.ToolDef{
		fs.ToolFSList(),
		fs.ToolFSRead(),
		fs.ToolFSGlob(),
		fs.ToolSearchText(),
		nav.ToolCodeSymbols(),
		fs.ToolDiffPreview(),
		task.ToolTaskResult(),
	})
}

// ListToolsForInvestigator returns the Investigator tool set: read-only tools + task.result + runtime.query.
// The Investigator can call runtime.query to correlate trace spans with CKG nodes.
func ListToolsForInvestigator() []llm.ToolDef {
	return applyParallelFlags(append(ListToolsForChild(), session.ToolRuntimeQuery()))
}

// ListToolsForMode returns tools for the given agent mode.
// hasSubtasks enables task.spawn/wait/cancel; hasQuestionAsker enables question tool.
func ListToolsForMode(mode string, caps Capabilities, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	switch mode {
	case "plan":
		return listToolsPlan(hasSubtasks, hasQuestionAsker)
	case "explore":
		return listToolsExplore()
	case "ask":
		return listToolsAsk(hasQuestionAsker)
	case "debug":
		return listToolsDebug(caps, hasSubtasks, hasQuestionAsker)
	case "architecture":
		return listToolsArchitecture(hasSubtasks, hasQuestionAsker)
	case "general":
		return listToolsGeneral(caps, hasSubtasks)
	case "orchestra":
		return listToolsOrchestra(hasSubtasks, hasQuestionAsker)
	case "worker":
		return listToolsWorker(caps)
	case "verifier":
		return listToolsVerifier(caps)
	case "product":
		return listToolsProduct(hasSubtasks, hasQuestionAsker)
	case "documentation":
		return listToolsDocs(hasQuestionAsker)
	case "scout":
		return listToolsScout()
	case "agent":
		// Mode agent is resolved to build|plan|explore|ask before tool listing;
		// if still seen here, treat as build.
		return listToolsBuild(caps, hasSubtasks, hasQuestionAsker)
	case "compaction", "title", "summary":
		return []llm.ToolDef{} // pure LLM output, no tools needed
	default: // "build" or ""
		return listToolsBuild(caps, hasSubtasks, hasQuestionAsker)
	}
}

func listToolsBuild(caps Capabilities, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(), fs.ToolFSDelete(), fs.ToolFSRename(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(), session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
	out = appendRepoMutatingTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsPlan(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	// fs.write is kept so the model can write .orchestra/plan.md — enforced at runtime.
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(), task.ToolPlanExit(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		// lsp.rename excluded: plan mode is read-only.
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsExplore() []llm.ToolDef {
	return applyParallelFlags([]llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		// lsp.rename excluded: explore mode is read-only.
		// task_result is appended for child explore via childToolsForSubagent.
	})
}

// listToolsAsk is Q&A read-only (stricter than explore: includes question when available).
func listToolsAsk(hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		// repo_map is the cheapest answer to "what is this project?" — one
		// call, a per-file outline under a byte budget. Without it the only
		// way to orient in a read-only mode was to read files one by one,
		// which is how a small context window gets spent on nothing.
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsArchitecture is design-only: plan md writes + research + optional research spawn.
func listToolsArchitecture(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(), task.ToolPlanExit(),
		session.ToolLessonPromote(), session.ToolPlaybookPromote(),
		session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsDebug is root-cause focused: full read/write + LSP + optional worker/explore spawn.
func listToolsDebug(caps Capabilities, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
	out = appendRepoMutatingTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsGeneral returns tools for the "general" multi-step execution subagent.
// It has full read+write access and reports results via task_result.
// todowrite is intentionally excluded — general agents track progress internally.
func listToolsGeneral(caps Capabilities, hasSubtasks bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(), fs.ToolFSDelete(), fs.ToolFSRename(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoRead(), session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(), task.ToolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
	out = appendRepoMutatingTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	return applyParallelFlags(out)
}

// FilterOrchestraLeadTools keeps only the Orchestra Lead allowlist
// (toolspec.Spec.Lead). The Lead delegates code, LSP and exec to workers;
// unknown ExtraTools and skills are dropped so the Lead schema stays small.
//
// contract_freeze and update_working_state are Lead-only by construction —
// agent.handleContractFreeze / handleUpdateWorkingState refuse every other
// mode. The agency tools are on the list and present only when the agency is
// on for the turn (the agent appends them; see agent.withAgencyTools).
func FilterOrchestraLeadTools(in []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, d := range in {
		name := d.Function.Name
		if !toolspec.IsLead(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, compactLeadToolDef(d))
	}
	return out
}

func compactLeadToolDef(d llm.ToolDef) llm.ToolDef {
	d.Function.Description = compactLeadDesc(d.Function.Description)
	if stripped := stripSchemaDescriptions(d.Function.Parameters); len(stripped) > 0 {
		d.Function.Parameters = stripped
	}
	return d
}

func compactLeadDesc(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".\n"); i > 0 && i < 120 {
		s = strings.TrimSpace(s[:i+1])
	}
	if len(s) > 120 {
		s = s[:119] + "…"
	}
	return s
}

// SmallContextTokens is the window at or below which a model is treated as
// small. It matches the circuit breaker's threshold so "small model" means
// one thing across the agent.
const SmallContextTokens = 32768

// CompactSchemasForSmallContext drops parameter descriptions from every tool
// schema, leaving the tool descriptions alone.
//
// On a 25k local model the schemas are most of what is charged before the
// first file is read — build mode spends 2838 bytes on the system prompt and
// 15939 on schemas. Around 2.5 KB of that is parameter prose that largely
// restates the type, enum and minimum sitting beside it, and the structure
// survives this untouched.
//
// Tool descriptions are deliberately kept. Reaching for the wrong tool is the
// mistake small models make most often, and that text is what prevents it;
// the Orchestra Lead truncates those too, but it has a fourteen-tool surface
// and a prompt that routes for it.
func CompactSchemasForSmallContext(defs []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, len(defs))
	for i, d := range defs {
		if stripped := stripSchemaDescriptions(d.Function.Parameters); len(stripped) > 0 {
			d.Function.Parameters = stripped
		}
		out[i] = d
	}
	return out
}

func stripSchemaDescriptions(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	stripDesc(v)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

func stripDesc(v any) {
	switch t := v.(type) {
	case map[string]any:
		delete(t, "description")
		for _, child := range t {
			stripDesc(child)
		}
	case []any:
		for _, child := range t {
			stripDesc(child)
		}
	}
}

// listToolsOrchestra is the Lead planner surface: read-only research, plan
// write, memory/promote, and delegation. No edit/LSP/bash/task_result.
func listToolsOrchestra(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSRead(), fs.ToolSearchText(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolFSWrite(),
		session.ToolMemoryRead(), session.ToolMemorySearch(),
		session.ToolLessonPromote(), session.ToolPlaybookPromote(),
		session.ToolUpdateWorkingState(), session.ToolContractFreeze(),
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(FilterOrchestraLeadTools(out))
}

// listToolsVerifier is goal-backward verification: read-only + diagnostics + optional bash.
func listToolsVerifier(caps Capabilities) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		git.ToolGitStatus(), git.ToolGitDiff(),
	}
	out = appendCapabilityTools(out, caps)
	return applyParallelFlags(out)
}

// listToolsProduct is the Product Lead surface (spec §3.2, routing matrix §7.1):
// repository reads for brownfield context, writes limited to .orchestra/product/
// (enforced by agent.checkProductEditScope), websearch/webfetch always listed —
// product discovery needs market research; runtime web consent still applies.
// No exec, no git-mutating tools. Spawn tools only with subtasks — as a
// child that is the agency's product > scout edge (tasks adds them), so its
// scouts can research competitors in parallel.
func listToolsProduct(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), nav.ToolRepoMap(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		task.ToolTaskResult(),
	}
	out = appendWebTools(out)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsDocs is the Docs Lead surface (spec §2.3.2, routing matrix §7.1):
// full repository reads for stack detect and brownfield docs, writes limited
// to conventions.md / .orchestra/docs/ / docs/ (enforced by
// agent.checkDocsEditScope). No web, no exec, no git mutators, no spawn.
func listToolsDocs(hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		task.ToolTaskResult(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsScout is the Market Scout (spec §2.1, stage 0): web research on
// competitors and the market, plus repository reads for a brownfield product.
// Web tools are always listed, as for Product — the runtime web consent still
// decides whether a call goes out, and the prompt tells the scout to mark
// claims it could not source as assumptions. No writes, no spawn.
func listToolsScout() []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), nav.ToolRepoMap(),
		task.ToolTaskResult(),
	}
	out = appendWebTools(out)
	return applyParallelFlags(out)
}

// listToolsWorker is the atomic implementer: edit/write + LSP, no nested spawn.
func listToolsWorker(caps Capabilities) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), nav.ToolRepoMap(), fs.ToolDiffPreview(),
		task.ToolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
	}
	out = appendCapabilityTools(out, caps)
	return applyParallelFlags(out)
}

// IsRegisteredTool reports whether name is a built-in tool, whichever modes
// offer it. The agent refuses a registered tool its mode did not offer, and
// leaves a name that is no tool at all to Call's "unknown tool" answer.
func IsRegisteredTool(name string) bool {
	registeredOnce.Do(func() {
		registeredNames = make(map[string]bool)
		for n := range allToolDefsMap() {
			registeredNames[n] = true
		}
	})
	return registeredNames[name]
}

var (
	registeredOnce  sync.Once
	registeredNames map[string]bool
)

// builtinDefs holds the constructor of every tool an agents: entry or a mode
// list can name (toolspec ByName and LeadOnly). Tools the agent builds per
// turn (skill_invoke, the agency tools) are not here.
var builtinDefs = map[string]func() llm.ToolDef{
	"ls": fs.ToolFSList, "read": fs.ToolFSRead, "glob": fs.ToolFSGlob,
	"write": fs.ToolFSWrite, "edit": fs.ToolFSEdit, "fs.delete": fs.ToolFSDelete, "fs.rename": fs.ToolFSRename,
	"ast_rename": fs.ToolASTRename, "diff.preview": fs.ToolDiffPreview, "grep": fs.ToolSearchText,
	"symbols": nav.ToolCodeSymbols, "explore": nav.ToolExploreCodebase, "repo_map": nav.ToolRepoMap,
	"semantic_search": nav.ToolSemanticSearch, "runtime_query": session.ToolRuntimeQuery,
	"lsp.definition": toolslsp.ToolLSPDefinition, "lsp.references": toolslsp.ToolLSPReferences,
	"lsp.hover": toolslsp.ToolLSPHover, "lsp.diagnostics": toolslsp.ToolLSPDiagnostics, "lsp.rename": toolslsp.ToolLSPRename,
	"todowrite": session.ToolTodoWrite, "todoread": session.ToolTodoRead,
	"memory_write": session.ToolMemoryWrite, "memory_read": session.ToolMemoryRead, "memory_search": session.ToolMemorySearch,
	"lesson_promote": session.ToolLessonPromote, "playbook_promote": session.ToolPlaybookPromote,
	"update_working_state": session.ToolUpdateWorkingState, "contract_freeze": session.ToolContractFreeze,
	"question": session.ToolQuestion, "plan_exit": task.ToolPlanExit,
	"git.status": git.ToolGitStatus, "git.log": git.ToolGitLog, "git.diff": git.ToolGitDiff, "git.worktree.list": git.ToolGitWorktreeList,
	"bash": exec.ToolExecRun, "bash.output": exec.ToolExecBashOutput, "bash.kill": exec.ToolExecBashKill,
	"gh.pr.list": git.ToolGHPRList, "gh.pr.view": git.ToolGHPRView, "gh.issue.list": git.ToolGHIssueList, "gh.issue.view": git.ToolGHIssueView,
	"git.commit": git.ToolGitCommit, "git.branch": git.ToolGitBranch, "git.checkout": git.ToolGitCheckout, "git.push": git.ToolGitPush,
	"git.worktree.add": git.ToolGitWorktreeAdd, "git.worktree.remove": git.ToolGitWorktreeRemove, "git.worktree.prune": git.ToolGitWorktreePrune,
	"gh.pr.create": git.ToolGHPRCreate,
	"webfetch":     web.ToolWebFetch, "websearch": web.ToolWebSearch,
	"browser.navigate": web.ToolBrowserNavigate, "browser.snapshot": web.ToolBrowserSnapshot, "browser.screenshot": web.ToolBrowserScreenshot,
	"browser.click": web.ToolBrowserClick, "browser.type": web.ToolBrowserType, "browser.fill": web.ToolBrowserFill,
	"browser.select": web.ToolBrowserSelect, "browser.eval": web.ToolBrowserEval, "browser.wait": web.ToolBrowserWait, "browser.close": web.ToolBrowserClose,
	"task": task.ToolTask, "task_spawn": task.ToolTaskSpawn, "task_wait": task.ToolTaskWait, "task_cancel": task.ToolTaskCancel,
	"task_result": task.ToolTaskResult,
}

// defs returns the definitions of names, in order. Every name must be in
// builtinDefs; TestBuiltinDefsMatchToolspec keeps the two in step.
func defs(names ...string) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(names))
	for _, n := range names {
		ctor, ok := builtinDefs[n]
		if !ok {
			panic("tools: no definition for built-in tool " + n)
		}
		out = append(out, ctor())
	}
	return out
}

// allToolDefsMap returns a map of every known tool definition keyed by its
// short canonical name (the name the LLM sees).
func allToolDefsMap() map[string]llm.ToolDef {
	m := make(map[string]llm.ToolDef, len(builtinDefs))
	for name, ctor := range builtinDefs {
		m[name] = ctor()
	}
	return m
}

// ResolveToolNames maps short tool names to their ToolDef structs.
// Returns an error if any name is unknown. The list of valid names is the same
// set exposed in config.validAgentToolNames.
func ResolveToolNames(names []string) ([]llm.ToolDef, error) {
	return ResolveToolNamesWithPolicy(names, Capabilities{Exec: true, Web: true, Browser: true})
}

// ResolveToolNamesWithPolicy is like ResolveToolNames but silently drops
// tools the runtime would deny by policy (allowExec / allowWeb /
// allowBrowser). This keeps the model from advertising tools it cannot
// actually call — without it, the skill loop wastes turns retrying
// denied tool calls until MaxDeniedToolRepeats trips.
//
// Unknown names still produce an error. Names that are present but
// gated-off are silently omitted.
func ResolveToolNamesWithPolicy(names []string, caps Capabilities) ([]llm.ToolDef, error) {
	m := allToolDefsMap()
	out := make([]llm.ToolDef, 0, len(names))
	for _, name := range names {
		d, ok := m[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool name: %q", name)
		}
		if !caps.allows(name) {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// allows reports whether the capabilities cover the consent a built-in tool
// needs. A name outside toolspec needs none here.
func (c Capabilities) allows(name string) bool {
	spec, ok := toolspec.Lookup(name)
	if !ok {
		return true
	}
	switch spec.Consent() {
	case toolspec.GroupExec:
		return c.Exec
	case toolspec.GroupWeb:
		return c.Web
	case toolspec.GroupBrowser:
		return c.Browser
	}
	return true
}

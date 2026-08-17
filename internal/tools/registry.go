package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/tools/git"
	"github.com/orchestra/orchestra/internal/tools/nav"
	"github.com/orchestra/orchestra/internal/tools/session"
	"github.com/orchestra/orchestra/internal/tools/task"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/internal/tools/web"
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

// appendExecTools adds bash + git-mutating + gh-mutating tools to out.
// Extracted in S3 (audit ledger, Sprint 6) so ListTools / listToolsBuild /
// listToolsGeneral share one definition instead of three copies that
// could drift independently.
func appendExecTools(out []llm.ToolDef) []llm.ToolDef {
	out = append(out, exec.ToolExecRun(), exec.ToolExecBashOutput(), exec.ToolExecBashKill())
	out = append(out, git.ToolGitCommit(), git.ToolGitBranch(), git.ToolGitCheckout(), git.ToolGitPush())
	out = append(out, git.ToolGitWorktreeAdd(), git.ToolGitWorktreeRemove(), git.ToolGitWorktreePrune())
	out = append(out,
		git.ToolGHPRList(), git.ToolGHPRCreate(), git.ToolGHPRView(),
		git.ToolGHIssueList(), git.ToolGHIssueView(),
	)
	return out
}

// appendWebTools adds web fetch + search tools to out. S3 in audit ledger.
func appendWebTools(out []llm.ToolDef) []llm.ToolDef {
	return append(out, web.ToolWebFetch(), web.ToolWebSearch())
}

// appendBrowserTools adds the 10 Playwright-MCP browser tools to out.
// S3 in audit ledger.
func appendBrowserTools(out []llm.ToolDef) []llm.ToolDef {
	return append(out,
		web.ToolBrowserNavigate(), web.ToolBrowserSnapshot(), web.ToolBrowserScreenshot(),
		web.ToolBrowserClick(), web.ToolBrowserType(), web.ToolBrowserFill(),
		web.ToolBrowserSelect(), web.ToolBrowserEval(), web.ToolBrowserWait(), web.ToolBrowserClose(),
	)
}

// appendSubtaskTools adds unified task + async spawn/wait/cancel to out.
func appendSubtaskTools(out []llm.ToolDef) []llm.ToolDef {
	return append(out, task.ToolTask(), task.ToolTaskSpawn(), task.ToolTaskWait(), task.ToolTaskCancel())
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
	return applyParallelFlags(out)
}

// parallelSafeTools and mutatingTools are the per-name classification
// of every built-in tool the agent ships. applyParallelFlags consults
// the two maps to decorate each ToolDef with ParallelSafe / Mutating.
//
// H1 in architecture audit: the previous design embedded these two
// lists in a switch statement inside applyParallelFlags. Tools missing
// from both lists silently fell into the conservative-default bucket
// (serial execution, not classed as a mutation) with no test failure.
// Hoisting to maps lets TestParallelFlags_AllBuiltinsClassified
// (registry_test.go) iterate every built-in constructor and assert it
// appears in exactly one map — a new tool added without registration
// fails the test immediately.
//
// MCP / plugin tools arriving via ExtraTools intentionally bypass this
// classification and keep the conservative default until explicitly
// added.
var parallelSafeTools = map[string]bool{
	"ls": true, "read": true, "glob": true, "grep": true,
	"symbols": true, "explore": true, "repo_map": true,
	"runtime_query":   true,
	"semantic_search": true,
	"webfetch":        true, "websearch": true,
	"lsp.definition": true, "lsp.references": true, "lsp.hover": true, "lsp.diagnostics": true,
	"diff.preview": true,
	"git.status":   true, "git.diff": true, "git.log": true, "git.worktree.list": true,
	"browser.snapshot": true, "browser.screenshot": true,
	"gh.pr.list": true, "gh.pr.view": true, "gh.issue.list": true, "gh.issue.view": true,
}

var mutatingTools = map[string]bool{
	"write": true, "edit": true,
	"bash": true, "bash.output": true, "bash.kill": true,
	"todowrite": true, "todoread": true, "update_working_state": true, "contract_freeze": true,
	"lesson_promote": true, "playbook_promote": true, "memory_write": true, "memory_read": true, "memory_search": true,
	"lsp.rename": true,
	"plan_exit":  true,
	"task_spawn": true, "task_wait": true, "task_cancel": true, "task_result": true, "task": true,
	"question":  true,
	"fs.delete": true, "fs.rename": true, "ast_rename": true,
	"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
	"git.worktree.add": true, "git.worktree.remove": true, "git.worktree.prune": true,
	"gh.pr.create":     true,
	"browser.navigate": true, "browser.click": true, "browser.type": true,
	"browser.fill": true, "browser.select": true, "browser.eval": true,
	"browser.wait": true, "browser.close": true,
	"skill_invoke": true,
}

func applyParallelFlags(defs []llm.ToolDef) []llm.ToolDef {
	for i := range defs {
		switch n := defs[i].Function.Name; {
		case parallelSafeTools[n]:
			defs[i].ParallelSafe = true
		case mutatingTools[n]:
			defs[i].Mutating = true
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
		return listToolsProduct(hasQuestionAsker)
	case "documentation":
		return listToolsDocs(hasQuestionAsker)
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(), session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		// lsp.rename excluded: explore mode is read-only.
		// task_result is appended for child explore via childToolsForSubagent.
	})
}

// listToolsAsk is Q&A read-only (stricter than explore: includes question when available).
func listToolsAsk(hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(),
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoRead(), session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(), task.ToolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
	}
	out = appendCapabilityTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	return applyParallelFlags(out)
}

// orchestraLeadToolNames is the strict Lead allowlist (≤14). Lead delegates
// code/LSP/exec to workers; ExtraTools (MCP, semantic_search, …) are filtered
// to this set in the agent layer.
var orchestraLeadToolNames = map[string]bool{
	"read": true, "grep": true, "explore": true, "repo_map": true, "write": true,
	"task": true, "task_spawn": true, "task_wait": true, "task_cancel": true, "question": true,
	"memory_read": true, "memory_search": true, "lesson_promote": true, "playbook_promote": true,
}

// FilterOrchestraLeadTools keeps only the Orchestra Lead allowlist. Unknown
// ExtraTools / skills are dropped so the Lead schema stays ≤14 tools.
func FilterOrchestraLeadTools(in []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(orchestraLeadToolNames))
	seen := make(map[string]bool, len(orchestraLeadToolNames))
	for _, d := range in {
		name := d.Function.Name
		if !orchestraLeadToolNames[name] || seen[name] {
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(),
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
// No exec, no git-mutating tools, no nested spawn.
func listToolsProduct(hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		task.ToolTaskResult(),
	}
	out = appendWebTools(out)
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
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(),
		session.ToolTodoWrite(), session.ToolTodoRead(),
		task.ToolTaskResult(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	if hasQuestionAsker {
		out = append(out, session.ToolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsWorker is the atomic implementer: edit/write + LSP, no nested spawn.
func listToolsWorker(caps Capabilities) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(),
		task.ToolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
	}
	out = appendCapabilityTools(out, caps)
	return applyParallelFlags(out)
}

// allToolDefsMap returns a map of every known tool definition keyed by its
// short canonical name (the name the LLM sees).
func allToolDefsMap() map[string]llm.ToolDef {
	all := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(), fs.ToolFSDelete(), fs.ToolFSRename(),
		fs.ToolSearchText(), nav.ToolCodeSymbols(), nav.ToolExploreCodebase(), fs.ToolDiffPreview(), session.ToolRuntimeQuery(),
		session.ToolTodoWrite(), session.ToolTodoRead(), session.ToolMemoryWrite(), session.ToolMemoryRead(), session.ToolMemorySearch(), session.ToolUpdateWorkingState(), exec.ToolExecRun(), exec.ToolExecBashOutput(), exec.ToolExecBashKill(), web.ToolWebFetch(), web.ToolWebSearch(), nav.ToolSemanticSearch(), nav.ToolRepoMap(), fs.ToolASTRename(),
		task.ToolTaskSpawn(), task.ToolTaskWait(), task.ToolTaskCancel(), task.ToolTaskResult(),
		task.ToolPlanExit(), session.ToolQuestion(), session.ToolContractFreeze(),
		session.ToolLessonPromote(), session.ToolPlaybookPromote(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(), git.ToolGitWorktreeList(),
		git.ToolGitCommit(), git.ToolGitBranch(), git.ToolGitCheckout(), git.ToolGitPush(),
		git.ToolGHPRList(), git.ToolGHPRCreate(), git.ToolGHPRView(), git.ToolGHIssueList(), git.ToolGHIssueView(),
		web.ToolBrowserNavigate(), web.ToolBrowserSnapshot(), web.ToolBrowserScreenshot(),
		web.ToolBrowserClick(), web.ToolBrowserType(), web.ToolBrowserFill(),
		web.ToolBrowserSelect(), web.ToolBrowserEval(), web.ToolBrowserWait(), web.ToolBrowserClose(),
	}
	m := make(map[string]llm.ToolDef, len(all))
	for _, d := range all {
		m[d.Function.Name] = d
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
		if !caps.Exec && isExecGated(name) {
			continue
		}
		if !caps.Web && isWebGated(name) {
			continue
		}
		if !caps.Browser && isBrowserGated(name) {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func isExecGated(name string) bool {
	switch name {
	case "bash", "exec.run", "bash_output", "bash_kill",
		"git.commit", "git.branch", "git.checkout", "git.push":
		return true
	}
	return false
}

func isWebGated(name string) bool {
	return name == "webfetch" || name == "websearch"
}

func isBrowserGated(name string) bool {
	return strings.HasPrefix(name, "browser.") || strings.HasPrefix(name, "browser_")
}

func mustSchema(s string) json.RawMessage {
	return toolschema.MustSchema(s)
}

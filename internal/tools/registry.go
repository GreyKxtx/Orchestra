package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/llm"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/internal/tools/git"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/internal/tools/toolslsp"
	"github.com/orchestra/orchestra/internal/tools/web"
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
	return append(out, toolTask(), toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
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
// ast_rename and repo_map but excluding plan_enter. The differences
// are intentional: ListTools is the "no mode preference" surface,
// listToolsBuild is the "actively coding" surface that includes the
// plan-to-build transition tool.
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
		toolCodeSymbols(),
		toolExploreCodebase(),
		ToolRepoMap(),
		fs.ToolDiffPreview(),
		toolRuntimeQuery(),
		toolTodoWrite(),
		toolTodoRead(),
		toolMemoryWrite(),
		toolMemoryRead(),
		toolMemorySearch(),
		toolslsp.ToolLSPDefinition(),
		toolslsp.ToolLSPReferences(),
		toolslsp.ToolLSPHover(),
		toolslsp.ToolLSPDiagnostics(),
		toolslsp.ToolLSPRename(),
		git.ToolGitStatus(),
		git.ToolGitLog(),
		git.ToolGitDiff(),
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
	"git.status":   true, "git.diff": true, "git.log": true,
	"browser.snapshot": true, "browser.screenshot": true,
	"gh.pr.list": true, "gh.pr.view": true, "gh.issue.list": true, "gh.issue.view": true,
}

var mutatingTools = map[string]bool{
	"write": true, "edit": true,
	"bash": true, "bash.output": true, "bash.kill": true,
	"todowrite": true, "todoread": true, "memory_write": true, "memory_read": true, "memory_search": true,
	"lsp.rename": true,
	"plan_exit":  true,
	"task_spawn": true, "task_wait": true, "task_cancel": true, "task_result": true, "task": true,
	"question":  true,
	"fs.delete": true, "fs.rename": true, "ast_rename": true,
	"git.commit": true, "git.branch": true, "git.checkout": true, "git.push": true,
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

// ToolSemanticSearch returns the semantic_search tool definition. Only
// added to the agent's tool list when embed.model is configured AND a
// CKG store is wired into the Runner.
func ToolSemanticSearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "semantic_search",
			Description: "Поиск по смыслу: эмбеддит query и возвращает top-K CKG-узлов (функции/методы/типы) по cosine similarity. Используй когда text-поиск (grep) не находит — например, ищешь концепт без точного имени. Требует индекса: orchestra ckg embed.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query":   { "type": "string", "minLength": 1 },
    "top_k":   { "type": "integer", "minimum": 1, "maximum": 50 },
    "snippet": { "type": "boolean", "description": "Включить фрагмент кода (первые 40 строк) каждого узла" }
  }
}`),
		},
		ParallelSafe: true,
	}
}

// ToolRepoMap returns the repo_map tool definition.
// no external dependencies beyond the tree-sitter grammars baked into the
// binary. Returns a compact outline of the workspace fitting an optional byte
// budget so the model can pick interesting files without first listing them.
func ToolRepoMap() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "repo_map",
			Description: "Быстрая карта репозитория: per-file outline (функции/типы/методы) по всем поддерживаемым языкам. Не требует индекса. Полезно для первичной ориентации перед ls/glob. budget_bytes ограничивает размер вывода (default 8192).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "budget_bytes":  { "type": "integer", "minimum": 256, "maximum": 65536, "description": "Max bytes of output. Smaller = pruned aggressively. Default 8192." },
    "max_files":     { "type": "integer", "minimum": 1, "maximum": 5000, "description": "Hard cap on files scanned. 0/omit = no cap." }
  }
}`),
		},
		ParallelSafe: true,
	}
}

// ToolSkillInvoke returns the skill_invoke tool definition with the
// caller-supplied list of valid skill names embedded in the JSON Schema
// enum. This narrows the model's choice and gives strict-schema providers
// the metadata to reject invalid skill names early.
func ToolSkillInvoke(skillNames []string) llm.ToolDef {
	skillProp := map[string]any{
		"type":        "string",
		"description": "Name of the skill to invoke (must match an available skill).",
	}
	if len(skillNames) > 0 {
		skillProp["enum"] = skillNames
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": skillProp,
			"task": map[string]any{
				"type":        "string",
				"description": "Task description / arguments passed to the skill. Becomes the user message and replaces $ARGUMENTS in the skill body.",
			},
		},
		"required":             []string{"skill", "task"},
		"additionalProperties": false,
	}
	raw, _ := json.Marshal(schema)
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "skill_invoke",
			Description: "Run a named skill synchronously as a child agent and return its result. Skills are reusable agent bundles (prompt + tools + model) loaded from .orchestra/skills/. Use this when a subtask matches an available skill's description.",
			Parameters:  raw,
		},
		Mutating: true,
	}
}

// ToolNames returns tool function names for prompt/debug usage.
func ToolNames(defs []llm.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Function.Name)
	}
	sort.Strings(out)
	return out
}

func toolCodeSymbols() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "symbols",
			Description: "Outline/символы файла (если доступно).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolExploreCodebase() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "explore",
			Description: "Три уровня глубины — выбираются автоматически по форме запроса:\n• Пакет: explore(\"internal/agent\") → все типы, методы, функции без кода тел\n• Тип: explore(\"Agent\") → определение struct/interface + полный список методов\n• Символ: explore(\"Agent.Run\") → полный код метода/функции + callers + callees\nДля метода пиши 'Agent.Run', не просто 'Run'. При неоднозначности — используй FQN из ответа.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["symbol_name"],
  "properties": {
    "symbol_name": {
      "type": "string",
      "description": "Пакет: 'internal/agent'. Тип: 'Agent'. Метод: 'Agent.Run'. Функция: 'ResolveExternalPatches'. FQN: 'internal/agent.Agent.Run'."
    }
  }
}`),
		},
	}
}


func toolTodoWrite() llm.ToolDef {
	fallback := "Обновить список задач (чеклист). Список отображается в каждом ходу — используй для отслеживания прогресса на длинных задачах."
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todowrite",
			Description: promptpkg.BuildToolDescription("todowrite", fallback),
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["todos"],
  "properties": {
    "todos": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "content", "status"],
        "properties": {
          "id":      { "type": "string", "minLength": 1 },
          "content": { "type": "string", "minLength": 1 },
          "status":  { "type": "string", "enum": ["pending", "in_progress", "done", "completed", "cancelled"] }
        }
      }
    }
  }
}`),
		},
	}
}

func toolTodoRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todoread",
			Description: "Прочитать текущий список задач.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
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
		toolCodeSymbols(),
		fs.ToolDiffPreview(),
		toolTaskResult(),
	})
}

// ListToolsForInvestigator returns the Investigator tool set: read-only tools + task.result + runtime.query.
// The Investigator can call runtime.query to correlate trace spans with CKG nodes.
func ListToolsForInvestigator() []llm.ToolDef {
	return applyParallelFlags(append(ListToolsForChild(), toolRuntimeQuery()))
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
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolMemoryWrite(), toolMemoryRead(), toolMemorySearch(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	out = appendCapabilityTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsPlan(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	// fs.write is kept so the model can write .orchestra/plan.md — enforced at runtime.
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolPlanExit(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		// lsp.rename excluded: plan mode is read-only.
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsExplore() []llm.ToolDef {
	return applyParallelFlags([]llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		// lsp.rename excluded: explore mode is read-only.
		// task_result is appended for child explore via childToolsForSubagent.
	})
}

// listToolsAsk is Q&A read-only (stricter than explore: includes question when available).
func listToolsAsk(hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsArchitecture is design-only: plan md writes + research + optional research spawn.
func listToolsArchitecture(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolPlanExit(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsDebug is root-cause focused: full read/write + LSP + optional worker/explore spawn.
func listToolsDebug(caps Capabilities, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	out = appendCapabilityTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsGeneral returns tools for the "general" multi-step execution subagent.
// It has full read+write access and reports results via task_result.
// todowrite is intentionally excluded — general agents track progress internally.
func listToolsGeneral(caps Capabilities, hasSubtasks bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(), fs.ToolFSDelete(), fs.ToolFSRename(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoRead(), toolMemoryWrite(), toolMemoryRead(), toolMemorySearch(), toolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	out = appendCapabilityTools(out, caps)
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	return applyParallelFlags(out)
}

// listToolsOrchestra is the Lead planner surface: research + plan write + task spawn, no production edit.
func listToolsOrchestra(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
	}
	if hasSubtasks {
		out = appendSubtaskTools(out)
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

// listToolsWorker is the atomic implementer: edit/write + LSP, no nested spawn.
func listToolsWorker(caps Capabilities) []llm.ToolDef {
	out := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(),
		toolTaskResult(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(),
	}
	out = appendCapabilityTools(out, caps)
	return applyParallelFlags(out)
}

func toolTask() llm.ToolDef {
	fallback := "Child agent (sync spawn+wait) for HEAVY/parallel work only. Prefer edit/write yourself for quick fixes. subagent_type: explore|ask|debug|architecture|general|worker. Do NOT use for 1–3 known-file edits."
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task",
			Description: promptpkg.BuildToolDescription("task", fallback),
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["prompt"] },
    { "required": ["goal"] }
  ],
  "properties": {
    "description": { "type": "string", "description": "Short 3-5 word label" },
    "prompt": { "type": "string", "minLength": 1, "description": "Detailed task or WorkOrder JSON for the child (or use goal)" },
    "goal": { "type": "string", "minLength": 1, "description": "Alias for prompt — provide exactly one of prompt/goal" },
    "subagent_type": {
      "type": "string",
      "enum": ["explore", "ask", "debug", "architecture", "general", "worker"],
      "description": "Child agent mode (default: explore)"
    },
    "tier": { "type": "string", "description": "Orchestra worker tier name (complex|focused|micro)" },
    "provider": { "type": "string", "description": "Optional named providers: map entry for child LLM" },
    "model": { "type": "string", "description": "Optional model id override for child LLM" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Wait timeout (default 120000)" }
  }
}`),
		},
	}
}

func toolTaskSpawn() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_spawn",
			Description: "Spawn a child asynchronously (rare). Prefer doing quick/concrete edits yourself with edit/write. Use only for parallel independent work; then task_wait.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["goal"] },
    { "required": ["prompt"] }
  ],
  "properties": {
    "goal": { "type": "string", "minLength": 1, "description": "Provide exactly one of goal/prompt" },
    "prompt": { "type": "string", "minLength": 1, "description": "Alias for goal" },
    "subagent_type": {
      "type": "string",
      "enum": ["explore", "ask", "debug", "architecture", "general", "worker"],
      "description": "Child agent mode (default: explore)"
    },
    "tier": { "type": "string", "description": "Orchestra worker tier name" },
    "provider": { "type": "string" },
    "model": { "type": "string" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Child lifetime (default 120000); 0 also uses 120000" }
  }
}`),
		},
	}
}

func toolTaskWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_wait",
			Description: "Подождать завершения дочерней задачи и получить её результат.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["task_id"],
  "properties": {
    "task_id": { "type": "string", "minLength": 1 },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolTaskCancel() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_cancel",
			Description: "Отменить дочернюю задачу.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["task_id"],
  "properties": {
    "task_id": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolTaskResult() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_result",
			Description: "Сообщить результат исследования родительскому агенту. Вызови когда закончил анализ.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

// ToolTaskResult is the public task_result tool (appended to child subagent tool lists).
func ToolTaskResult() llm.ToolDef { return toolTaskResult() }

func toolRuntimeQuery() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "runtime_query",
			Description: "Получить spans OTel-трейса с привязкой к узлам CKG (code_file, code_lineno, node_fqn). Используй для диагностики багов по trace_id.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["trace_id"],
  "properties": {
    "trace_id": {
      "type": "string",
      "minLength": 1,
      "description": "Hex trace_id из OTel (128-бит, 32 символа)"
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 1000,
      "description": "Максимальное число spans (по умолчанию 500)"
    }
  }
}`),
		},
	}
}

func toolPlanEnter() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan_enter",
			Description: "Переключиться в режим ПЛАНИРОВАНИЯ (read-only). Используй для детального анализа задачи перед внесением изменений.",
			Parameters:  mustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func toolPlanExit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan_exit",
			Description: "Завершить планирование и запросить переключение в build-режим. Вызывай только когда план в {{PLAN_PATH}} полностью готов.",
			Parameters:  mustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func toolQuestion() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "question",
			Description: "Задать пользователю уточняющий вопрос (блокирует выполнение до ответа). Используй для критичных трейдоффов при планировании.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["questions"],
  "properties": {
    "questions": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["question"],
        "properties": {
          "question": {"type": "string", "minLength": 1},
          "options":  {"type": "array", "items": {"type": "string"}}
        }
      }
    }
  }
}`),
		},
	}
}


func toolMemoryWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_write",
			Description: "Сохранить факт в постоянную память. scope=project → .orchestra/memory/agent.md. Начните с [pin] для sticky facts. scope=session → память сессии.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": { "type": "string", "minLength": 1, "description": "Факт или контекст для сохранения" },
    "scope":   { "type": "string", "enum": ["project", "session"], "description": "project (default) или session" }
  }
}`),
		},
	}
}

func toolMemoryRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_read",
			Description: "Прочитать слоистую память проекта (ORCHESTRA.md, .orchestra/memory/, session, global). Без аргументов — список источников. Экономит контекст vs полный inject.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer":  { "type": "string", "enum": ["orchestra", "session", "repo", "global", "all"], "description": "Слой памяти" },
    "path":   { "type": "string", "description": "ORCHESTRA.md или .orchestra/memory/agent.md" },
    "max_kb": { "type": "integer", "minimum": 1, "maximum": 64, "description": "Лимит ответа в KiB" }
  }
}`),
		},
	}
}

func toolMemorySearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_search",
			Description: "Поиск по слоям памяти (agent.md, session, global, ORCHESTRA.md) по подстроке. Для точных фактов без полного memory_read.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query": { "type": "string", "minLength": 1, "description": "Подстрока поиска" },
    "limit": { "type": "integer", "minimum": 1, "maximum": 20, "description": "Макс. хитов (default 8)" }
  }
}`),
		},
	}
}

// allToolDefsMap returns a map of every known tool definition keyed by its
// short canonical name (the name the LLM sees).
func allToolDefsMap() map[string]llm.ToolDef {
	all := []llm.ToolDef{
		fs.ToolFSList(), fs.ToolFSRead(), fs.ToolFSGlob(), fs.ToolFSWrite(), fs.ToolFSEdit(), fs.ToolFSDelete(), fs.ToolFSRename(),
		fs.ToolSearchText(), toolCodeSymbols(), toolExploreCodebase(), fs.ToolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolMemoryWrite(), toolMemoryRead(), toolMemorySearch(), exec.ToolExecRun(), exec.ToolExecBashOutput(), exec.ToolExecBashKill(), web.ToolWebFetch(), web.ToolWebSearch(), ToolSemanticSearch(), ToolRepoMap(), fs.ToolASTRename(),
		toolTaskSpawn(), toolTaskWait(), toolTaskCancel(), toolTaskResult(),
		toolPlanEnter(), toolPlanExit(), toolQuestion(),
		toolslsp.ToolLSPDefinition(), toolslsp.ToolLSPReferences(), toolslsp.ToolLSPHover(), toolslsp.ToolLSPDiagnostics(), toolslsp.ToolLSPRename(),
		git.ToolGitStatus(), git.ToolGitLog(), git.ToolGitDiff(),
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

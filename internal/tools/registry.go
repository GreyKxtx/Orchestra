package tools

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/orchestra/orchestra/internal/llm"
)

// ListTools returns OpenAI-compatible tool definitions (JSON Schema), filtered by policy.
// Only tools returned here may be exposed to the model.
func ListTools(allowExec, allowWeb, allowBrowser bool) []llm.ToolDef {
	out := []llm.ToolDef{
		toolFSList(),
		toolFSRead(),
		toolFSGlob(),
		toolFSWrite(),
		toolFSEdit(),
		toolFSDelete(),
		toolFSRename(),
		toolSearchText(),
		toolCodeSymbols(),
		toolExploreCodebase(),
		toolDiffPreview(),
		toolRuntimeQuery(),
		toolTodoWrite(),
		toolTodoRead(),
		toolMemoryWrite(),
		toolLSPDefinition(),
		toolLSPReferences(),
		toolLSPHover(),
		toolLSPDiagnostics(),
		toolLSPRename(),
		toolGitStatus(),
		toolGitLog(),
		toolGitDiff(),
	}
	if allowExec {
		out = append(out, toolExecRun())
		out = append(out, toolExecBashOutput(), toolExecBashKill())
		out = append(out, toolGitCommit(), toolGitBranch(), toolGitCheckout(), toolGitPush())
		out = append(out,
			toolGHPRList(), toolGHPRCreate(), toolGHPRView(),
			toolGHIssueList(), toolGHIssueView(),
		)
	}
	if allowWeb {
		out = append(out, toolWebFetch())
		out = append(out, toolWebSearch())
	}
	if allowBrowser {
		out = append(out,
			toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
			toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
			toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
		)
	}
	return applyParallelFlags(out)
}

// applyParallelFlags decorates each ToolDef with ParallelSafe / Mutating flags
// based on the tool's name. Centralised so adding a new tool means updating
// only this switch — the rest of the agent infrastructure reads the flags
// declaratively without knowing tool names.
//
// Default (unknown name) is the conservative pair {ParallelSafe=false,
// Mutating=false}: such a tool gets executed serially (no parallel batching)
// but isn't classed as a permission-bearing mutation either. MCP/plugin tools
// land in this default bucket until explicitly classified.
func applyParallelFlags(defs []llm.ToolDef) []llm.ToolDef {
	for i := range defs {
		switch defs[i].Function.Name {
		// Pure reads — safe to fan out concurrently.
		case "ls", "read", "glob", "grep", "symbols", "explore", "semantic_search",
			"todoread", "task.result", "runtime.query", "webfetch", "websearch",
			"lsp.definition", "lsp.references", "lsp.hover", "lsp.diagnostics",
			"diff.preview",
			"git.status", "git.diff", "git.log",
			"browser.snapshot", "browser.screenshot",
			"gh.pr.list", "gh.pr.view", "gh.issue.list", "gh.issue.view":
			defs[i].ParallelSafe = true
		// State-mutating tools — must run one at a time.
		case "write", "edit", "bash", "bash.output", "bash.kill", "todowrite", "memory_write",
			"lsp.rename", "plan.enter", "plan.exit",
			"task.spawn", "task.wait", "task.cancel", "question",
			"fs.delete", "fs.rename",
			"git.commit", "git.branch", "git.checkout", "git.push",
			"gh.pr.create",
			"browser.navigate", "browser.click", "browser.type", "browser.fill",
			"browser.select", "browser.eval", "browser.wait", "browser.close",
			"skill_invoke":
			defs[i].Mutating = true
		}
	}
	return defs
}

// ListToolsWithMCP appends MCP server tools to the base tool list.
func ListToolsWithMCP(allowExec, allowWeb, allowBrowser bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
	out := ListTools(allowExec, allowWeb, allowBrowser)
	return append(out, mcpDefs...)
}

// ListToolsWithSubtasksAndMCP returns parent-agent tools including subtask and MCP tools.
func ListToolsWithSubtasksAndMCP(allowExec, allowWeb, allowBrowser bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
	out := ListToolsWithSubtasks(allowExec, allowWeb, allowBrowser)
	return append(out, mcpDefs...)
}

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

func toolFSList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "ls",
			Description: "Список файлов в workspace (с exclude правилами).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "path": { "type": "string" },
    "recursive": { "type": "boolean" },
    "max_entries": { "type": "integer", "minimum": 0 },
    "exclude_dirs": { "type": "array", "items": { "type": "string" } },
    "include_hash": { "type": "boolean" },
    "limit": { "type": "integer", "minimum": 0 },
    "skip_backups": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolFSRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "read",
			Description: "Читает файл в workspace и возвращает content+sha256 (file_hash). Содержимое возвращается с префиксами номеров строк (`1: строка`) — только для ориентации. Префиксы не входят в файл: не включай их в поле `search` при редактировании и не пиши их в content при записи. При усечении (truncated=true) нумеруются только вернувшиеся строки.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "max_bytes": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolFSGlob() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "glob",
			Description: "Поиск файлов по glob-паттерну (поддерживает ** для рекурсивного поиска).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["pattern"],
  "properties": {
    "pattern": { "type": "string", "minLength": 1 },
    "limit": { "type": "integer", "minimum": 0 },
    "include_hash": { "type": "boolean" },
    "exclude_dirs": { "type": "array", "items": { "type": "string" } }
  }
}`),
		},
	}
}

func toolFSWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "write",
			Description: "Атомарная запись файла (создание или перезапись). Для создания нового файла используй must_not_exist=true. Для перезаписи — file_hash текущей версии (из read). Контент пишется как есть — не включай префиксы номеров строк из read.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "content"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "content": { "type": "string" },
    "file_hash": { "type": "string" },
    "must_not_exist": { "type": "boolean" },
    "backup": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolFSEdit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "edit",
			Description: "Точечная замена в файле (search → replace). Строка поиска должна быть уникальна в файле. При неоднозначности — AmbiguousMatch; если не найдена — StaleContent. file_hash рекомендуется для защиты от гонок. Поле `search` должно содержать точный текст файла без префиксов номеров строк.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "search", "replace"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "search": { "type": "string", "minLength": 1 },
    "replace": { "type": "string" },
    "file_hash": { "type": "string" },
    "backup": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolSearchText() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "grep",
			Description: "Текстовый поиск по проекту. Возвращает исчерпывающий список всех совпадений — если показано N результатов, других нет. Каждый матч в .go файле содержит поле [в: Receiver.Method] — это метод/функция где найдена строка. Не повторяй запрос если результат уже получен.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query": { "type": "string", "minLength": 1 },
    "paths": { "type": "array", "items": { "type": "string" } },
    "max_matches": { "type": "integer", "minimum": 0 },
    "exclude_dirs": { "type": "array", "items": { "type": "string" } },
    "options": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "max_matches_per_file": { "type": "integer", "minimum": 0 },
        "case_insensitive": { "type": "boolean" },
        "context_lines": { "type": "integer", "minimum": 0 }
      }
    }
  }
}`),
		},
	}
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

func toolExecRun() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash",
			Description: "Запуск команды внутри workspace (sandboxed: timeout/output limit). Установи run_in_background=true для длительных задач (build/test/dev-server) — вернётся bg_id, который можно опрашивать через bash.output и убивать через bash.kill.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["command"],
  "properties": {
    "command": { "type": "string", "minLength": 1 },
    "args": { "type": "array", "items": { "type": "string" } },
    "workdir": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 },
    "output_limit_kb": { "type": "integer", "minimum": 0 },
    "run_in_background": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolExecBashOutput() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.output",
			Description: "Возвращает накопленный с прошлого опроса stdout/stderr и статус (running/done/killed/timed_out) фонового процесса. Установи peek=true чтобы прочитать не сдвигая курсор.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["bg_id"],
  "properties": {
    "bg_id": { "type": "string", "minLength": 1 },
    "peek": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolExecBashKill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.kill",
			Description: "Терминирует фоновый процесс по bg_id. Уже завершённый процесс — no-op с актуальным статусом.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["bg_id"],
  "properties": {
    "bg_id": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolTodoWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todowrite",
			Description: "Обновить список задач (чеклист). Список отображается в каждом ходу — используй для отслеживания прогресса на длинных задачах.",
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
          "status":  { "type": "string", "enum": ["pending", "in_progress", "done", "cancelled"] }
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
func ListToolsWithSubtasks(allowExec, allowWeb, allowBrowser bool) []llm.ToolDef {
	out := ListTools(allowExec, allowWeb, allowBrowser)
	out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	return applyParallelFlags(out)
}

// ListToolsForChild returns a restricted read-only tool set for child agents plus task.result.
// Child agents cannot write files, run commands, or spawn further subtasks.
func ListToolsForChild() []llm.ToolDef {
	return applyParallelFlags([]llm.ToolDef{
		toolFSList(),
		toolFSRead(),
		toolFSGlob(),
		toolSearchText(),
		toolCodeSymbols(),
		toolDiffPreview(),
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
func ListToolsForMode(mode string, allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	switch mode {
	case "plan":
		return listToolsPlan(hasSubtasks, hasQuestionAsker)
	case "explore":
		return listToolsExplore()
	case "general":
		return listToolsGeneral(allowExec, allowWeb, allowBrowser, hasSubtasks)
	case "compaction", "title", "summary":
		return []llm.ToolDef{} // pure LLM output, no tools needed
	default: // "build" or ""
		return listToolsBuild(allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker)
	}
}

func listToolsBuild(allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(), toolFSEdit(), toolFSDelete(), toolFSRename(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolMemoryWrite(), toolPlanEnter(),
		toolLSPDefinition(), toolLSPReferences(), toolLSPHover(), toolLSPDiagnostics(), toolLSPRename(),
		toolGitStatus(), toolGitLog(), toolGitDiff(),
	}
	if allowExec {
		out = append(out, toolExecRun())
		out = append(out, toolExecBashOutput(), toolExecBashKill())
		out = append(out, toolGitCommit(), toolGitBranch(), toolGitCheckout(), toolGitPush())
		out = append(out,
			toolGHPRList(), toolGHPRCreate(), toolGHPRView(),
			toolGHIssueList(), toolGHIssueView(),
		)
	}
	if allowWeb {
		out = append(out, toolWebFetch())
		out = append(out, toolWebSearch())
	}
	if allowBrowser {
		out = append(out,
			toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
			toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
			toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
		)
	}
	if hasSubtasks {
		out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsPlan(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	// fs.write is kept so the model can write .orchestra/plan.md — enforced at runtime.
	out := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolPlanExit(),
		toolLSPDefinition(), toolLSPReferences(), toolLSPHover(), toolLSPDiagnostics(),
		// lsp.rename excluded: plan mode is read-only.
	}
	if hasSubtasks {
		out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return applyParallelFlags(out)
}

func listToolsExplore() []llm.ToolDef {
	return applyParallelFlags([]llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(),
		toolSearchText(), toolCodeSymbols(), toolTaskResult(),
		toolLSPDefinition(), toolLSPReferences(), toolLSPHover(), toolLSPDiagnostics(),
		// lsp.rename excluded: explore mode is read-only.
	})
}

// listToolsGeneral returns tools for the "general" multi-step execution subagent.
// It has full read+write access and reports results via task_result.
// todowrite is intentionally excluded — general agents track progress internally.
func listToolsGeneral(allowExec, allowWeb, allowBrowser, hasSubtasks bool) []llm.ToolDef {
	out := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(), toolFSEdit(), toolFSDelete(), toolFSRename(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolDiffPreview(), toolRuntimeQuery(),
		toolTodoRead(), toolMemoryWrite(), toolTaskResult(),
		toolLSPDefinition(), toolLSPReferences(), toolLSPHover(), toolLSPDiagnostics(), toolLSPRename(),
		toolGitStatus(), toolGitLog(), toolGitDiff(),
	}
	if allowExec {
		out = append(out, toolExecRun())
		out = append(out, toolExecBashOutput(), toolExecBashKill())
		out = append(out, toolGitCommit(), toolGitBranch(), toolGitCheckout(), toolGitPush())
		out = append(out,
			toolGHPRList(), toolGHPRCreate(), toolGHPRView(),
			toolGHIssueList(), toolGHIssueView(),
		)
	}
	if allowWeb {
		out = append(out, toolWebFetch())
		out = append(out, toolWebSearch())
	}
	if allowBrowser {
		out = append(out,
			toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
			toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
			toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
		)
	}
	if hasSubtasks {
		out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	}
	return applyParallelFlags(out)
}

func toolTaskSpawn() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_spawn",
			Description: "Создать дочернюю задачу для независимого исследования. Возвращает task_id. Используй task_wait для получения результата.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["goal"],
  "properties": {
    "goal": { "type": "string", "minLength": 1 },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0 }
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
			Description: "Завершить планирование и запросить переключение в build-режим. Вызывай только когда план в .orchestra/plan.md полностью готов.",
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

func toolWebFetch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "webfetch",
			Description: "Загрузить URL и вернуть текстовое содержимое страницы. Поддерживаются только http/https. Приватные, loopback и link-local адреса заблокированы.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["url"],
  "properties": {
    "url": { "type": "string", "minLength": 1, "description": "Полный URL (http:// или https://)" },
    "max_bytes": { "type": "integer", "minimum": 0, "description": "Максимальный размер ответа в байтах" }
  }
}`),
		},
	}
}

func toolWebSearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "websearch",
			Description: "Поиск в интернете. Возвращает список результатов с заголовком, URL и сниппетом. Требует настройки web.search.provider и web.search.api_key в .orchestra.yml.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query":       { "type": "string", "minLength": 1, "description": "Поисковый запрос." },
    "max_results": { "type": "integer", "minimum": 1, "maximum": 20, "description": "Максимум результатов. По умолчанию из конфига (5)." }
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
			Description: "Сохранить факт в постоянную память агента (.orchestra/memory/agent.md). Используй для запоминания важных решений, предпочтений пользователя или контекста, который нужен в следующих сессиях. Не используй для временных заметок — для этого todowrite.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": { "type": "string", "minLength": 1, "description": "Факт или контекст для сохранения" }
  }
}`),
		},
	}
}

// allToolDefsMap returns a map of every known tool definition keyed by its
// short canonical name (the name the LLM sees).
func allToolDefsMap() map[string]llm.ToolDef {
	all := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(), toolFSEdit(), toolFSDelete(), toolFSRename(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolDiffPreview(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolMemoryWrite(), toolExecRun(), toolExecBashOutput(), toolExecBashKill(), toolWebFetch(), toolWebSearch(), ToolSemanticSearch(),
		toolTaskSpawn(), toolTaskWait(), toolTaskCancel(), toolTaskResult(),
		toolPlanEnter(), toolPlanExit(), toolQuestion(),
		toolLSPDefinition(), toolLSPReferences(), toolLSPHover(), toolLSPDiagnostics(), toolLSPRename(),
		toolGitStatus(), toolGitLog(), toolGitDiff(),
		toolGitCommit(), toolGitBranch(), toolGitCheckout(), toolGitPush(),
		toolGHPRList(), toolGHPRCreate(), toolGHPRView(), toolGHIssueList(), toolGHIssueView(),
		toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
		toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
		toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
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
	m := allToolDefsMap()
	out := make([]llm.ToolDef, 0, len(names))
	for _, name := range names {
		d, ok := m[name]
		if !ok {
			return nil, fmt.Errorf("unknown tool name: %q", name)
		}
		out = append(out, d)
	}
	return out, nil
}

func toolLSPDefinition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.definition",
			Description: "Перейти к определению символа (функции, типа, переменной) в указанной позиции файла (1-based line/col). Использует gopls или другой настроенный LSP-сервер.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col"],
  "properties": {
    "path": { "type": "string", "minLength": 1, "description": "Путь к файлу относительно workspace root" },
    "line": { "type": "integer", "minimum": 1, "description": "Строка (1-based)" },
    "col":  { "type": "integer", "minimum": 1, "description": "Колонка — байтовый offset (1-based)" }
  }
}`),
		},
	}
}

func toolLSPReferences() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.references",
			Description: "Найти все места использования символа в проекте (1-based line/col). Использует LSP-сервер.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "line": { "type": "integer", "minimum": 1 },
    "col":  { "type": "integer", "minimum": 1 },
    "include_declaration": { "type": "boolean", "description": "Включить объявление в результаты (default: false)" }
  }
}`),
		},
	}
}

func toolLSPHover() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.hover",
			Description: "Получить документацию/тип символа в указанной позиции (hover-info). Использует LSP-сервер.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "line": { "type": "integer", "minimum": 1 },
    "col":  { "type": "integer", "minimum": 1 }
  }
}`),
		},
	}
}

func toolLSPDiagnostics() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.diagnostics",
			Description: "Получить диагностические ошибки и предупреждения LSP-сервера для файла (аналог 'Problems' в IDE). Возвращает массив диагностик с позициями и уровнем severity.",
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

func toolLSPRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.rename",
			Description: "Переименовать символ во всём проекте. Возвращает список предложенных правок (edits), которые нужно применить через fs.edit или fs.write.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col", "new_name"],
  "properties": {
    "path":     { "type": "string", "minLength": 1 },
    "line":     { "type": "integer", "minimum": 1 },
    "col":      { "type": "integer", "minimum": 1 },
    "new_name": { "type": "string", "minLength": 1, "description": "Новое имя символа" }
  }
}`),
		},
	}
}

func toolDiffPreview() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "diff.preview",
			Description: "Предварительный просмотр изменений: применяет search→replace в памяти и возвращает unified diff без записи на диск. Используй перед edit чтобы убедиться что замена правильная.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "search", "replace"],
  "properties": {
    "path":    { "type": "string", "minLength": 1, "description": "Путь к файлу относительно workspace root" },
    "search":  { "type": "string", "minLength": 1, "description": "Текст для поиска (как в edit)" },
    "replace": { "type": "string", "description": "Текст замены" }
  }
}`),
		},
	}
}

func toolFSDelete() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.delete",
			Description: "Удалить файл или директорию по workspace-relative пути. Для непустых директорий требуется recursive=true.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path":      { "type": "string", "minLength": 1, "description": "Workspace-relative путь для удаления." },
    "recursive": { "type": "boolean", "description": "Рекурсивно удалить непустую директорию. По умолчанию false." }
  }
}`),
		},
	}
}

func toolFSRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.rename",
			Description: "Переместить или переименовать файл/директорию внутри workspace. Родительские директории new_path создаются автоматически.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "new_path"],
  "properties": {
    "path":     { "type": "string", "minLength": 1, "description": "Workspace-relative путь источника." },
    "new_path": { "type": "string", "minLength": 1, "description": "Workspace-relative путь назначения." }
  }
}`),
		},
	}
}

func toolGitStatus() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.status",
			Description: "Show current git working-tree status — staged/unstaged changes, untracked files, current branch.",
			Parameters:  mustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func toolGitLog() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.log",
			Description: "Show commit history. n limits the count (default 20, max 200). Optionally filtered by ref or path.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "n":       { "type": "integer", "minimum": 1, "maximum": 200, "description": "Max commits to show. Default 20." },
    "ref":     { "type": "string", "description": "Branch, tag, or commit hash." },
    "path":    { "type": "string", "description": "Limit to commits touching this workspace-relative path." },
    "oneline": { "type": "boolean", "description": "Compact single-line format." }
  }
}`),
		},
	}
}

func toolGitCommit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.commit",
			Description: "Stage files and create a git commit. Use add=[\".\"] to stage all changes.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["message"],
  "properties": {
    "message":     { "type": "string", "minLength": 1, "description": "Commit message." },
    "add":         { "type": "array", "items": {"type":"string"}, "description": "Workspace-relative paths to git add. Use [\".\"] for all changes." },
    "allow_empty": { "type": "boolean", "description": "Allow commit with no staged changes." }
  }
}`),
		},
	}
}

func toolGitBranch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.branch",
			Description: "List, create, or delete a local branch. Defaults to listing when no option is set.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "list":   { "type": "boolean", "description": "List local branches (default)." },
    "create": { "type": "string",  "description": "Create a branch with this name." },
    "delete": { "type": "string",  "description": "Delete a branch with this name." }
  }
}`),
		},
	}
}

func toolGitCheckout() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.checkout",
			Description: "Switch to a branch/commit or restore specific files. new_branch creates and switches (-b).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "ref":        { "type": "string", "description": "Branch, tag, or commit to switch to." },
    "paths":      { "type": "array",  "items": {"type":"string"}, "description": "Workspace-relative paths to restore from HEAD." },
    "new_branch": { "type": "string", "description": "Create this branch and switch to it (-b)." }
  }
}`),
		},
	}
}

func toolGitPush() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.push",
			Description: "Push current branch to remote. force=true uses --force-with-lease (safer than --force). Default remote is 'origin'.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "remote":       { "type": "string",  "description": "Remote name. Default 'origin'." },
    "branch":       { "type": "string",  "description": "Branch to push. Default: current branch." },
    "set_upstream": { "type": "boolean", "description": "Set upstream tracking (-u)." },
    "force":        { "type": "boolean", "description": "Push with --force-with-lease." }
  }
}`),
		},
	}
}

func toolGitDiff() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.diff",
			Description: "Show diff of uncommitted changes. staged=true shows staged (--cached) changes. ref compares against a specific commit or branch.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "staged": { "type": "boolean", "description": "Show staged (--cached) changes instead of unstaged." },
    "ref":    { "type": "string", "description": "Compare against this commit, branch, or tag." },
    "path":   { "type": "string", "description": "Limit diff to this workspace-relative file or directory." }
  }
}`),
		},
	}
}

func toolBrowserNavigate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.navigate",
			Description: "Открыть URL в браузере и дождаться загрузки страницы.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["url"],
  "properties": {
    "url": { "type": "string", "minLength": 1 },
    "wait_until": { "type": "string", "enum": ["load", "domcontentloaded", "networkidle"] }
  }
}`),
		},
	}
}

func toolBrowserSnapshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.snapshot",
			Description: "Вернуть accessibility-дерево текущей страницы (структурированный текст с ref-идентификаторами для кликов).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func toolBrowserScreenshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.screenshot",
			Description: "Снять скриншот текущей страницы (base64 PNG).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "full_page": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserClick() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.click",
			Description: "Нажать на элемент по имени или ref из snapshot.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserType() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.type",
			Description: "Ввести текст в поле ввода (по имени или ref из snapshot).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["text"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "text": { "type": "string" },
    "clear": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserFill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.fill",
			Description: "Заполнить несколько полей формы за один вызов.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["fields"],
  "properties": {
    "fields": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["value"],
        "properties": {
          "element": { "type": "string" },
          "ref": { "type": "string" },
          "value": { "type": "string" }
        }
      }
    }
  }
}`),
		},
	}
}

func toolBrowserSelect() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.select",
			Description: "Выбрать опцию в выпадающем списке <select>.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["value"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "value": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserEval() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.eval",
			Description: "Выполнить JavaScript в контексте страницы. Требует allow_eval: true в конфигурации.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["expression"],
  "properties": {
    "expression": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolBrowserWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.wait",
			Description: "Ждать условие: совпадение URL, появление CSS-селектора или текста на странице.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "url": { "type": "string" },
    "selector": { "type": "string" },
    "text": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolBrowserClose() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.close",
			Description: "Закрыть текущую страницу (браузер остаётся запущенным для повторного использования).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func toolGHPRList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.list",
			Description: "List pull requests in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","merged","all"], "description": "Filter by PR state. Default: open." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max PRs to return. Default: 20." },
    "base":   { "type": "string", "description": "Filter by base branch name." }
  }
}`),
		},
	}
}

func toolGHPRCreate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.create",
			Description: "Create a pull request from the current branch. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title"],
  "properties": {
    "title": { "type": "string", "minLength": 1, "description": "PR title." },
    "body":  { "type": "string", "description": "PR description (markdown)." },
    "base":  { "type": "string", "description": "Base branch. Defaults to repo default branch." },
    "draft": { "type": "boolean", "description": "Create as draft PR." }
  }
}`),
		},
	}
}

func toolGHPRView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.view",
			Description: "View details of a pull request including description and comments. number=0 uses the current branch's PR.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "number": { "type": "integer", "minimum": 0, "description": "PR number. Omit or 0 = current branch's PR." }
  }
}`),
		},
	}
}

func toolGHIssueList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.list",
			Description: "List issues in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","all"], "description": "Filter by state. Default: open." },
    "labels": { "type": "array", "items": { "type": "string" }, "description": "Filter by label names." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max issues to return. Default: 20." }
  }
}`),
		},
	}
}

func toolGHIssueView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.view",
			Description: "View details of a GitHub issue including description and comments.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["number"],
  "properties": {
    "number": { "type": "integer", "minimum": 1, "description": "Issue number." }
  }
}`),
		},
	}
}

func mustSchema(s string) json.RawMessage {
	// Validate schema JSON at startup (panic is OK: it's a programmer error).
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return json.RawMessage(s)
}

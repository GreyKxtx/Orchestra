package tools

import (
	"encoding/json"
	"sort"

	"github.com/orchestra/orchestra/internal/llm"
)

// ListTools returns OpenAI-compatible tool definitions (JSON Schema), filtered by policy.
// Only tools returned here may be exposed to the model.
func ListTools(allowExec bool) []llm.ToolDef {
	out := []llm.ToolDef{
		toolFSList(),
		toolFSRead(),
		toolFSGlob(),
		toolFSWrite(),
		toolFSEdit(),
		toolSearchText(),
		toolCodeSymbols(),
		toolExploreCodebase(),
		toolRuntimeQuery(),
		toolTodoWrite(),
		toolTodoRead(),
	}
	if allowExec {
		out = append(out, toolExecRun())
	}
	return out
}

// ListToolsWithMCP appends MCP server tools to the base tool list.
func ListToolsWithMCP(allowExec bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
	out := ListTools(allowExec)
	return append(out, mcpDefs...)
}

// ListToolsWithSubtasksAndMCP returns parent-agent tools including subtask and MCP tools.
func ListToolsWithSubtasksAndMCP(allowExec bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
	out := ListToolsWithSubtasks(allowExec)
	return append(out, mcpDefs...)
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
			Description: "Текстовый поиск по проекту (можно ограничить paths).",
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
			Description: "Поиск архитектурного контекста. Используй, чтобы найти код функции, структуры или интерфейса по имени, а также посмотреть, где они используются в проекте. НЕ используй для чтения целых файлов.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["symbol_name"],
  "properties": {
    "symbol_name": {
      "type": "string",
      "description": "Точное имя функции, класса или интерфейса (например: UpsertFile или UserRepository)"
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
			Description: "Запуск команды внутри workspace (sandboxed: timeout/output limit).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["command"],
  "properties": {
    "command": { "type": "string", "minLength": 1 },
    "args": { "type": "array", "items": { "type": "string" } },
    "workdir": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 },
    "output_limit_kb": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolTodoWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todo.write",
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
			Name:        "todo.read",
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
func ListToolsWithSubtasks(allowExec bool) []llm.ToolDef {
	out := ListTools(allowExec)
	out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	return out
}

// ListToolsForChild returns a restricted read-only tool set for child agents plus task.result.
// Child agents cannot write files, run commands, or spawn further subtasks.
func ListToolsForChild() []llm.ToolDef {
	return []llm.ToolDef{
		toolFSList(),
		toolFSRead(),
		toolFSGlob(),
		toolSearchText(),
		toolCodeSymbols(),
		toolTaskResult(),
	}
}

// ListToolsForInvestigator returns the Investigator tool set: read-only tools + task.result + runtime.query.
// The Investigator can call runtime.query to correlate trace spans with CKG nodes.
func ListToolsForInvestigator() []llm.ToolDef {
	return append(ListToolsForChild(), toolRuntimeQuery())
}

// ListToolsForMode returns tools for the given agent mode.
// mode: "build" (default, full access), "plan" (read-only + plan tools), "explore" (subagent).
// hasSubtasks enables task.spawn/wait/cancel; hasQuestionAsker enables question tool.
func ListToolsForMode(mode string, allowExec, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	switch mode {
	case "plan":
		return listToolsPlan(hasSubtasks, hasQuestionAsker)
	case "explore":
		return listToolsExplore()
	default: // "build" or ""
		return listToolsBuild(allowExec, hasSubtasks, hasQuestionAsker)
	}
}

func listToolsBuild(allowExec, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	out := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(), toolFSEdit(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolPlanEnter(),
	}
	if allowExec {
		out = append(out, toolExecRun())
	}
	if hasSubtasks {
		out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return out
}

func listToolsPlan(hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
	// fs.write is kept so the model can write .orchestra/plan.md — enforced at runtime.
	out := []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(), toolFSWrite(),
		toolSearchText(), toolCodeSymbols(), toolExploreCodebase(), toolRuntimeQuery(),
		toolTodoWrite(), toolTodoRead(), toolPlanExit(),
	}
	if hasSubtasks {
		out = append(out, toolTaskSpawn(), toolTaskWait(), toolTaskCancel())
	}
	if hasQuestionAsker {
		out = append(out, toolQuestion())
	}
	return out
}

func listToolsExplore() []llm.ToolDef {
	return []llm.ToolDef{
		toolFSList(), toolFSRead(), toolFSGlob(),
		toolSearchText(), toolCodeSymbols(), toolTaskResult(),
	}
}

func toolTaskSpawn() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task.spawn",
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
			Name:        "task.wait",
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
			Name:        "task.cancel",
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
			Name:        "task.result",
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
			Name:        "runtime.query",
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
			Name:        "plan.enter",
			Description: "Переключиться в режим ПЛАНИРОВАНИЯ (read-only). Используй для детального анализа задачи перед внесением изменений.",
			Parameters:  mustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func toolPlanExit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan.exit",
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

func mustSchema(s string) json.RawMessage {
	// Validate schema JSON at startup (panic is OK: it's a programmer error).
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return json.RawMessage(s)
}

package exec

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

// ToolExecRun returns the bash tool definition.
func ToolExecRun() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash",
			Description: "Запуск команды внутри workspace (sandboxed: timeout/output limit). Установи run_in_background=true для длительных задач (build/test/dev-server) — вернётся bg_id, который можно опрашивать через bash.output и убивать через bash.kill.",
			Parameters: toolschema.MustSchema(`{
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

// ToolExecBashOutput returns the bash.output tool definition.
func ToolExecBashOutput() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.output",
			Description: "Возвращает накопленный с прошлого опроса stdout/stderr и статус (running/done/killed/timed_out) фонового процесса. Установи peek=true чтобы прочитать не сдвигая курсор.",
			Parameters: toolschema.MustSchema(`{
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

// ToolExecBashKill returns the bash.kill tool definition.
func ToolExecBashKill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.kill",
			Description: "Терминирует фоновый процесс по bg_id. Уже завершённый процесс — no-op с актуальным статусом.",
			Parameters: toolschema.MustSchema(`{
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

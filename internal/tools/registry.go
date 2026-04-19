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
		toolSearchText(),
		toolCodeSymbols(),
	}
	if allowExec {
		out = append(out, toolExecRun())
	}
	return out
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
			Name:        "fs.list",
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
			Name:        "fs.read",
			Description: "Читает файл в workspace и возвращает content+sha256 (file_hash).",
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

func toolSearchText() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "search.text",
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
			Name:        "code.symbols",
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

func toolExecRun() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "exec.run",
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

func mustSchema(s string) json.RawMessage {
	// Validate schema JSON at startup (panic is OK: it's a programmer error).
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return json.RawMessage(s)
}

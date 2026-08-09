package toolslsp

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolLSPDefinition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.definition",
			Description: "Перейти к определению символа (функции, типа, переменной) в указанной позиции файла (1-based line/col). Использует gopls или другой настроенный LSP-сервер.",
			Parameters: toolschema.MustSchema(`{
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

func ToolLSPReferences() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.references",
			Description: "Найти все места использования символа в проекте (1-based line/col). Использует LSP-сервер.",
			Parameters: toolschema.MustSchema(`{
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

func ToolLSPHover() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.hover",
			Description: "Получить документацию/тип символа в указанной позиции (hover-info). Использует LSP-сервер.",
			Parameters: toolschema.MustSchema(`{
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

func ToolLSPDiagnostics() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.diagnostics",
			Description: "Получить диагностические ошибки и предупреждения LSP-сервера для файла (аналог 'Problems' в IDE). Возвращает массив диагностик с позициями и уровнем severity.",
			Parameters: toolschema.MustSchema(`{
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

func ToolLSPRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "lsp.rename",
			Description: "Переименовать символ во всём проекте. Возвращает список предложенных правок (edits), которые нужно применить через fs.edit или fs.write.",
			Parameters: toolschema.MustSchema(`{
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

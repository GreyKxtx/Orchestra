package fs

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolFSList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "ls",
			Description: "Список файлов в workspace (с exclude правилами).",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "read",
			Description: "Читает файл в workspace и возвращает content+sha256 (file_hash). Содержимое возвращается с префиксами номеров строк (`1: строка`) — только для ориентации. Префиксы не входят в файл: не включай их в поле `search` при редактировании и не пиши их в content при записи. При усечении (truncated=true) нумеруются только вернувшиеся строки.",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSGlob() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "glob",
			Description: "Поиск файлов по glob-паттерну (поддерживает ** для рекурсивного поиска).",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "write",
			Description: "Атомарная запись файла (создание или перезапись). Для создания нового файла используй must_not_exist=true. Для перезаписи — file_hash текущей версии (из read). Контент пишется как есть — не включай префиксы номеров строк из read.",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSEdit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "edit",
			Description: "Точечная замена в файле (search → replace). Строка поиска должна быть уникальна в файле. При неоднозначности — AmbiguousMatch; если не найдена — StaleContent. file_hash рекомендуется для защиты от гонок. Поле `search` должно содержать точный текст файла без префиксов номеров строк.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "search", "replace"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "search": { "type": "string", "minLength": 1 },
    "replace": { "type": "string" },
    "file_hash": { "type": "string" },
    "target_symbol": { "type": "string", "description": "Optional CKG symbol name — search uniqueness is checked inside this function/method only (Planner–Worker WorkOrder)" },
    "backup": { "type": "boolean" }
  }
}`),
		},
	}
}

func ToolSearchText() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "grep",
			Description: "Текстовый поиск по проекту. Возвращает исчерпывающий список всех совпадений — если показано N результатов, других нет. Каждый матч в .go файле содержит поле [в: Receiver.Method] — это метод/функция где найдена строка. Не повторяй запрос если результат уже получен.",
			Parameters: toolschema.MustSchema(`{
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

func ToolDiffPreview() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "diff.preview",
			Description: "Предварительный просмотр изменений: применяет search→replace в памяти и возвращает unified diff без записи на диск. Используй перед edit чтобы убедиться что замена правильная.",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSDelete() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.delete",
			Description: "Удалить файл или директорию по workspace-relative пути. Для непустых директорий требуется recursive=true.",
			Parameters: toolschema.MustSchema(`{
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

func ToolFSRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "fs.rename",
			Description: "Переместить или переименовать файл/директорию внутри workspace. Родительские директории new_path создаются автоматически.",
			Parameters: toolschema.MustSchema(`{
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

func ToolASTRename() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "ast_rename",
			Description: "AST-aware rename идентификатора в одном файле через tree-sitter. Пропускает строки/комментарии/подстроки. Поддерживает все языки, которые есть в CKG (go/ts/py/rs/java/...). Маршрутизируется через ту же staging/file_hash логику, что fs.write. Когда нужно — используй вместо edit для имён, которые встречаются и как подстроки.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "old_name", "new_name"],
  "properties": {
    "path":     { "type": "string", "minLength": 1 },
    "old_name": { "type": "string", "minLength": 1 },
    "new_name": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

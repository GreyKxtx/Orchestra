package session

import (
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolTodoWrite() llm.ToolDef {
	fallback := "Обновить список задач (чеклист). Список отображается в каждом ходу — используй для отслеживания прогресса на длинных задачах."
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todowrite",
			Description: promptpkg.BuildToolDescription("todowrite", fallback),
			Parameters: toolschema.MustSchema(`{
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

func ToolTodoRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "todoread",
			Description: "Прочитать текущий список задач.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func ToolMemoryWrite() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_write",
			Description: "Сохранить факт в постоянную память. scope=project → .orchestra/memory/agent.md. Начните с [pin] для sticky facts. scope=session → память сессии.",
			Parameters: toolschema.MustSchema(`{
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

func ToolMemoryRead() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_read",
			Description: "Прочитать слоистую память проекта (ORCHESTRA.md, .orchestra/memory/, session, global). Без аргументов — список источников. Экономит контекст vs полный inject.",
			Parameters: toolschema.MustSchema(`{
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

func ToolMemorySearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "memory_search",
			Description: "Поиск по слоям памяти (agent.md, session, global, ORCHESTRA.md) по подстроке. Для точных фактов без полного memory_read.",
			Parameters: toolschema.MustSchema(`{
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

func ToolRuntimeQuery() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "runtime_query",
			Description: "Получить spans OTel-трейса с привязкой к узлам CKG (code_file, code_lineno, node_fqn). Используй для диагностики багов по trace_id.",
			Parameters: toolschema.MustSchema(`{
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

func ToolQuestion() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "question",
			Description: "Задать пользователю уточняющий вопрос (блокирует выполнение до ответа). Используй для критичных трейдоффов при планировании.",
			Parameters: toolschema.MustSchema(`{
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

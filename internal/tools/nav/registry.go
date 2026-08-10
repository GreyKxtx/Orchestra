package nav

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolCodeSymbols() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "symbols",
			Description: "Outline/символы файла (если доступно).",
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

func ToolExploreCodebase() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "explore",
			Description: "Три уровня глубины — выбираются автоматически по форме запроса:\n• Пакет: explore(\"internal/agent\") → все типы, методы, функции без кода тел\n• Тип: explore(\"Agent\") → определение struct/interface + полный список методов\n• Символ: explore(\"Agent.Run\") → полный код метода/функции + callers + callees\nДля метода пиши 'Agent.Run', не просто 'Run'. При неоднозначности — используй FQN из ответа.",
			Parameters: toolschema.MustSchema(`{
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

func ToolSemanticSearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "semantic_search",
			Description: "Поиск по смыслу: эмбеддит query и возвращает top-K CKG-узлов (функции/методы/типы) по cosine similarity. Используй когда text-поиск (grep) не находит — например, ищешь концепт без точного имени. Требует индекса: orchestra ckg embed.",
			Parameters: toolschema.MustSchema(`{
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

func ToolRepoMap() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "repo_map",
			Description: "Быстрая карта репозитория: per-file outline (функции/типы/методы) по всем поддерживаемым языкам. Не требует индекса. Полезно для первичной ориентации перед ls/glob. budget_bytes ограничивает размер вывода (default 8192).",
			Parameters: toolschema.MustSchema(`{
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

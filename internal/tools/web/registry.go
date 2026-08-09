package web

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolWebFetch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "webfetch",
			Description: "Загрузить URL и вернуть текстовое содержимое страницы. Поддерживаются только http/https. Приватные, loopback и link-local адреса заблокированы.",
			Parameters: toolschema.MustSchema(`{
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

func ToolWebSearch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "websearch",
			Description: "Поиск в интернете. Возвращает список результатов с заголовком, URL и сниппетом. Требует настройки web.search.provider и web.search.api_key в .orchestra.yml.",
			Parameters: toolschema.MustSchema(`{
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
func ToolBrowserNavigate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.navigate",
			Description: "Открыть URL в браузере и дождаться загрузки страницы.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserSnapshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.snapshot",
			Description: "Вернуть accessibility-дерево текущей страницы (структурированный текст с ref-идентификаторами для кликов).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func ToolBrowserScreenshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.screenshot",
			Description: "Снять скриншот текущей страницы (base64 PNG).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "full_page": { "type": "boolean" }
  }
}`),
		},
	}
}

func ToolBrowserClick() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.click",
			Description: "Нажать на элемент по имени или ref из snapshot.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserType() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.type",
			Description: "Ввести текст в поле ввода (по имени или ref из snapshot).",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserFill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.fill",
			Description: "Заполнить несколько полей формы за один вызов.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserSelect() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.select",
			Description: "Выбрать опцию в выпадающем списке <select>.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserEval() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.eval",
			Description: "Выполнить JavaScript в контексте страницы. Требует allow_eval: true в конфигурации.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.wait",
			Description: "Ждать условие: совпадение URL, появление CSS-селектора или текста на странице.",
			Parameters: toolschema.MustSchema(`{
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

func ToolBrowserClose() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.close",
			Description: "Закрыть текущую страницу (браузер остаётся запущенным для повторного использования).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

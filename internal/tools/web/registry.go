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
			Description: "Fetch a URL and return the page as text. Only http/https are allowed; private, loopback and link-local addresses are blocked.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["url"],
  "properties": {
    "url": { "type": "string", "minLength": 1, "description": "Full URL (http:// or https://)" },
    "max_bytes": { "type": "integer", "minimum": 0, "description": "Maximum response size in bytes" }
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
			Description: "Search the web. Returns results with a title, URL and snippet. Requires web.search.provider and web.search.api_key in .orchestra.yml.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query":       { "type": "string", "minLength": 1, "description": "The search query." },
    "max_results": { "type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum results to return. Defaults to the configured value (5)." }
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
			Description: "Open a URL in the browser and wait for the page to load.",
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
			Description: "Return the current page's accessibility tree: structured text carrying the ref ids you click by.",
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
			Description: "Take a screenshot of the current page (base64 PNG).",
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
			Description: "Click an element, addressed by name or by a ref from snapshot.",
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
			Description: "Type text into an input, addressed by name or by a ref from snapshot.",
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
			Description: "Fill several form fields in one call.",
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
			Description: "Choose an option in a <select> dropdown.",
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
			Description: "Evaluate JavaScript in the page context. Requires allow_eval: true in the config.",
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
			Description: "Wait for a condition: a URL match, a CSS selector appearing, or text appearing on the page.",
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
			Description: "Close the current page. The browser stays running for reuse.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

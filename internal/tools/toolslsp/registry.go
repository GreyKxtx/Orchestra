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
			Description: "Jump to the definition of the symbol at a file position (1-based line/col). Uses gopls or whichever LSP server is configured.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col"],
  "properties": {
    "path": { "type": "string", "minLength": 1, "description": "File path relative to the workspace root" },
    "line": { "type": "integer", "minimum": 1, "description": "Line number (1-based)" },
    "col":  { "type": "integer", "minimum": 1, "description": "Column as a byte offset (1-based)" }
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
			Description: "Find every use of the symbol at a file position (1-based line/col), project-wide. Uses the LSP server.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col"],
  "properties": {
    "path": { "type": "string", "minLength": 1 },
    "line": { "type": "integer", "minimum": 1 },
    "col":  { "type": "integer", "minimum": 1 },
    "include_declaration": { "type": "boolean", "description": "Include the declaration itself in the results (default: false)" }
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
			Description: "Get the type and documentation of the symbol at a position (hover info). Uses the LSP server.",
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
			Description: "Get the LSP server's errors and warnings for a file — the equivalent of an IDE's Problems panel. Returns diagnostics with positions and severity.",
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
			Description: "Rename a symbol project-wide. Returns the proposed edits, which you then apply with edit or write.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "line", "col", "new_name"],
  "properties": {
    "path":     { "type": "string", "minLength": 1 },
    "line":     { "type": "integer", "minimum": 1 },
    "col":      { "type": "integer", "minimum": 1 },
    "new_name": { "type": "string", "minLength": 1, "description": "The symbol's new name" }
  }
}`),
		},
	}
}

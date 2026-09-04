package session

import (
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolTodoWrite() llm.ToolDef {
	fallback := "Update the task checklist. The list is shown on every turn — use it to track progress through long tasks."
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
			Description: "Read the current task checklist.",
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
			Description: "Store a fact in durable memory. scope=project → .orchestra/memory/agent.md; scope=session → this session's memory; scope=<dept> (engineering, frontend@web, …) → episodic lessons (.orchestra/memory/lessons/<dept>.md, max 3 per run, 400 chars). Prefix with [pin] for facts that must survive compaction.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": { "type": "string", "minLength": 1, "description": "The fact or context to store" },
    "scope":   { "type": "string", "description": "project (default), session, or dept key (engineering, qa@prod, …)" }
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
			Description: "Read the project's layered memory (ORCHESTRA.md, .orchestra/memory/, dept lessons, session, global). With no arguments it lists the available sources. Cheaper than injecting everything.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "layer":  { "type": "string", "enum": ["orchestra", "session", "repo", "lessons", "global", "all"], "description": "Which memory layer to read" },
    "path":   { "type": "string", "description": "ORCHESTRA.md, .orchestra/memory/agent.md or .orchestra/memory/lessons/<dept>.md" },
    "max_kb": { "type": "integer", "minimum": 1, "maximum": 64, "description": "Response cap in KiB" }
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
			Description: "Search across the memory layers (agent.md, session, global, ORCHESTRA.md, dept lessons). Use it to pull one fact without reading a whole layer.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query": { "type": "string", "minLength": 1, "description": "Text to search for" },
    "limit": { "type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum hits to return (default 8)" }
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
			Description: "Fetch the spans of an OTel trace, resolved onto CKG nodes (code_file, code_lineno, node_fqn). Use it to diagnose a bug from a trace_id.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["trace_id"],
  "properties": {
    "trace_id": {
      "type": "string",
      "minLength": 1,
      "description": "Hex OTel trace_id (128-bit, 32 characters)"
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 1000,
      "description": "Maximum number of spans (default 500)"
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
			Description: "Ask the user a clarifying question; execution blocks until they answer. Use it for trade-offs during planning that you cannot decide alone.",
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

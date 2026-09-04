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
			Description: "Outline of a file's symbols. Resolution order: LSP document symbols → tree-sitter (Go, when built with CGO) → regex (Go). Non-Go files with no LSP return an empty list.",
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
			Description: "Three levels of detail, chosen automatically from the shape of the query:\n• Package: explore(\"internal/agent\") → every type, method and function, without bodies\n• Type: explore(\"Agent\") → the struct/interface definition plus its full method list\n• Symbol: explore(\"Agent.Run\") → the full body plus a multi-hop subgraph of callers and callees\nOptional: depth (1..4, default 2), direction (downstream|upstream|both).\nFor a method write 'Agent.Run', not just 'Run'. If the name is ambiguous, re-query with the FQN from the response.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["symbol_name"],
  "properties": {
    "symbol_name": {
      "type": "string",
      "description": "Package: 'internal/agent'. Type: 'Agent'. Method: 'Agent.Run'. Function: 'ResolveExternalPatches'. FQN: 'internal/agent.Agent.Run'."
    },
    "depth": {
      "type": "integer",
      "minimum": 1,
      "maximum": 4,
      "description": "How many hops to traverse, 1..4 (default 2)"
    },
    "direction": {
      "type": "string",
      "enum": ["downstream", "upstream", "both", "callees", "callers"],
      "description": "downstream (what it calls / callees) | upstream (what calls it / callers) | both"
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
			Description: "Search by meaning: embeds the query and returns the top-K CKG nodes (functions, methods, types) by cosine similarity. Use it when grep fails because you are after a concept rather than an exact name. Requires an index — run `orchestra ckg embed`.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query":   { "type": "string", "minLength": 1 },
    "top_k":   { "type": "integer", "minimum": 1, "maximum": 50 },
    "snippet": { "type": "boolean", "description": "Include a code snippet (first 40 lines) for each node" }
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
			Description: "Fast repository map: a per-file outline of functions, types and methods across every supported language. Needs no index. Use it to orient yourself before reaching for ls or glob. budget_bytes caps the output size (default 8192).",
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

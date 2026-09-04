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
			Description: "List files in the workspace, honouring exclude rules.",
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
			Description: "Read a file in the workspace; returns content plus its sha256 (file_hash). Lines come prefixed with numbers (`1: text`) for orientation only. The prefixes are not part of the file: never include them in `search` when editing, or in `content` when writing. When truncated=true, only the returned lines are numbered.",
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
			Description: "Find files by glob pattern (** matches recursively).",
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
			Description: "Write a file atomically (create or overwrite). To create a new file pass must_not_exist=true; to overwrite, pass the file_hash of the version you read. Content is written verbatim — do not include the line-number prefixes that read adds.",
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
			Description: "Replace one exact region of a file (search → replace). The search string must be unique in the file: more than one match returns AmbiguousMatch, none returns StaleContent. Pass file_hash to guard against a concurrent change. `search` must be the file's exact text, without the line-number prefixes read adds.",
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
			Description: "Text search across the project. The result is exhaustive: if N matches are shown, there are no others. Matches in .go files carry [in: Receiver.Method] naming the enclosing function. Do not repeat a search you already have the result of.",
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
			Description: "Preview a change: applies search→replace in memory and returns a unified diff without touching disk. Use it before edit to confirm the replacement is right.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "search", "replace"],
  "properties": {
    "path":    { "type": "string", "minLength": 1, "description": "File path relative to the workspace root" },
    "search":  { "type": "string", "minLength": 1, "description": "Text to search for (same rules as edit)" },
    "replace": { "type": "string", "description": "Replacement text" }
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
			Description: "Delete a file or directory by workspace-relative path. A non-empty directory requires recursive=true.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path":      { "type": "string", "minLength": 1, "description": "Workspace-relative path to delete." },
    "recursive": { "type": "boolean", "description": "Delete a non-empty directory recursively. Defaults to false." }
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
			Description: "Move or rename a file or directory inside the workspace. Parent directories of new_path are created automatically.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "new_path"],
  "properties": {
    "path":     { "type": "string", "minLength": 1, "description": "Workspace-relative source path." },
    "new_path": { "type": "string", "minLength": 1, "description": "Workspace-relative destination path." }
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
			Description: "AST-aware rename of an identifier within one file, via tree-sitter. Skips string literals, comments and substring hits. Works for every language the CKG parses (go/ts/py/rs/java/...). Goes through the same staging and file_hash path as write. Prefer it over edit for names that also occur as substrings.",
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

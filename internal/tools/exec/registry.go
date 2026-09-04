package exec

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

// ToolExecRun returns the bash tool definition.
func ToolExecRun() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash",
			Description: "Run a command inside the workspace, sandboxed by a timeout and an output limit. For long-running work (build, test, dev server) pass run_in_background=true: it returns a bg_id you can poll with bash.output and stop with bash.kill.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["command"],
  "properties": {
    "command": { "type": "string", "minLength": 1 },
    "args": { "type": "array", "items": { "type": "string" } },
    "workdir": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 },
    "output_limit_kb": { "type": "integer", "minimum": 0 },
    "run_in_background": { "type": "boolean" }
  }
}`),
		},
	}
}

// ToolExecBashOutput returns the bash.output tool definition.
func ToolExecBashOutput() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.output",
			Description: "Return the stdout/stderr a background process produced since the last poll, plus its status (running/done/killed/timed_out). Pass peek=true to read without advancing the cursor.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["bg_id"],
  "properties": {
    "bg_id": { "type": "string", "minLength": 1 },
    "peek": { "type": "boolean" }
  }
}`),
		},
	}
}

// ToolExecBashKill returns the bash.kill tool definition.
func ToolExecBashKill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "bash.kill",
			Description: "Terminate a background process by bg_id. A process that already finished is a no-op returning its final status.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["bg_id"],
  "properties": {
    "bg_id": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

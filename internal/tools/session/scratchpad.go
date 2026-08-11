package session

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

// ToolUpdateWorkingState is orchestra Lead-only (handled in-process by agent).
func ToolUpdateWorkingState() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name: "update_working_state",
			Description: "Replace the orchestra Lead scratchpad (.orchestra/state.md). Use to track Goal/Done/Next/Notes across long tasks. " +
				"Not shown to the user directly — keeps your planning context compact. Full markdown body replaces the file.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": {
      "type": "string",
      "minLength": 1,
      "description": "Full markdown for .orchestra/state.md (Goal, Done, Next, Notes sections)"
    }
  }
}`),
		},
		Mutating: true,
	}
}

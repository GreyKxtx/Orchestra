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
				"Not shown to the user directly — keeps your planning context compact. Full markdown body replaces the file. " +
				"Pass dept (e.g. frontend@web) to maintain a department-instance scratchpad at .orchestra/depts/{instance}.md instead.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": {
      "type": "string",
      "minLength": 1,
      "description": "Full markdown for the scratchpad (Goal, Done, Next, Notes sections)"
    },
    "dept": {
      "type": "string",
      "pattern": "^[a-z0-9][a-z0-9_-]*(@[a-z0-9][a-z0-9_-]*)?$",
      "description": "Department instance id (frontend, frontend@web). Writes .orchestra/depts/{instance}.md instead of .orchestra/state.md"
    }
  }
}`),
		},
		Mutating: true,
	}
}

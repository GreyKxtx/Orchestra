package session

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolLessonPromote() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name: "lesson_promote",
			Description: "Promote the latest dept pattern lesson (or explicit note) into a draft local playbook overlay " +
				"(.orchestra/playbooks/local/{dept}.md) with decision_ref PENDING:…. " +
				"After User approval via open_questions, the runtime auto-seals decision_ref from decisions.md; then call playbook_promote.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["dept"],
  "properties": {
    "dept":   { "type": "string", "description": "Department key (engineering, frontend@web, …)" },
    "note":   { "type": "string", "description": "Optional explicit overlay body; default = last pattern lesson" },
    "source": { "type": "string", "description": "Optional source path for audit (e.g. lessons file)" }
  }
}`),
		},
	}
}

func ToolPlaybookPromote() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name: "playbook_promote",
			Description: "Merge an approved local overlay into the dept L2 playbook (.orchestra/playbooks/{dept}.md). " +
				"Requires overlay decision_ref approved in decisions.md. promotion_ref is optional (defaults to decision_ref).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["dept"],
  "properties": {
    "dept":           { "type": "string", "description": "Department key" },
    "promotion_ref":  { "type": "string", "description": "Optional; defaults to approved overlay decision_ref" }
  }
}`),
		},
	}
}

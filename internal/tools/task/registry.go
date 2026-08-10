package task

import (
	"encoding/json"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolTask() llm.ToolDef {
	fallback := "Child agent (sync spawn+wait) for HEAVY/parallel work only. Prefer edit/write yourself for quick fixes. subagent_type: explore|ask|debug|architecture|general|worker. Do NOT use for 1–3 known-file edits."
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task",
			Description: promptpkg.BuildToolDescription("task", fallback),
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["prompt"] },
    { "required": ["goal"] }
  ],
  "properties": {
    "description": { "type": "string", "description": "Short 3-5 word label" },
    "prompt": { "type": "string", "minLength": 1, "description": "Detailed task or WorkOrder JSON for the child (or use goal)" },
    "goal": { "type": "string", "minLength": 1, "description": "Alias for prompt — provide exactly one of prompt/goal" },
    "subagent_type": {
      "type": "string",
      "enum": ["explore", "ask", "debug", "architecture", "general", "worker"],
      "description": "Child agent mode (default: explore)"
    },
    "tier": { "type": "string", "description": "Orchestra worker tier name (complex|focused|micro)" },
    "provider": { "type": "string", "description": "Optional named providers: map entry for child LLM" },
    "model": { "type": "string", "description": "Optional model id override for child LLM" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Wait timeout (default 120000)" }
  }
}`),
		},
	}
}

func ToolTaskSpawn() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_spawn",
			Description: "Spawn a child asynchronously (rare). Prefer doing quick/concrete edits yourself with edit/write. Use only for parallel independent work; then task_wait.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["goal"] },
    { "required": ["prompt"] }
  ],
  "properties": {
    "goal": { "type": "string", "minLength": 1, "description": "Provide exactly one of goal/prompt" },
    "prompt": { "type": "string", "minLength": 1, "description": "Alias for goal" },
    "subagent_type": {
      "type": "string",
      "enum": ["explore", "ask", "debug", "architecture", "general", "worker"],
      "description": "Child agent mode (default: explore)"
    },
    "tier": { "type": "string", "description": "Orchestra worker tier name" },
    "provider": { "type": "string" },
    "model": { "type": "string" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Child lifetime (default 120000); 0 also uses 120000" }
  }
}`),
		},
	}
}

func ToolTaskWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_wait",
			Description: "Подождать завершения дочерней задачи и получить её результат.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["task_id"],
  "properties": {
    "task_id": { "type": "string", "minLength": 1 },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func ToolTaskCancel() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_cancel",
			Description: "Отменить дочернюю задачу.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["task_id"],
  "properties": {
    "task_id": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func ToolTaskResult() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_result",
			Description: "Сообщить результат исследования родительскому агенту. Вызови когда закончил анализ.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["content"],
  "properties": {
    "content": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func ToolPlanEnter() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan_enter",
			Description: "Переключиться в режим ПЛАНИРОВАНИЯ (read-only). Используй для детального анализа задачи перед внесением изменений.",
			Parameters:  toolschema.MustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func ToolPlanExit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan_exit",
			Description: "Завершить планирование и запросить переключение в build-режим. Вызывай только когда план в {{PLAN_PATH}} полностью готов.",
			Parameters:  toolschema.MustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

// ToolSkillInvoke returns the skill_invoke tool definition with the
// caller-supplied list of valid skill names embedded in the JSON Schema enum.
func ToolSkillInvoke(skillNames []string) llm.ToolDef {
	skillProp := map[string]any{
		"type":        "string",
		"description": "Name of the skill to invoke (must match an available skill).",
	}
	if len(skillNames) > 0 {
		skillProp["enum"] = skillNames
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": skillProp,
			"task": map[string]any{
				"type":        "string",
				"description": "Task description / arguments passed to the skill. Becomes the user message and replaces $ARGUMENTS in the skill body.",
			},
		},
		"required":             []string{"skill", "task"},
		"additionalProperties": false,
	}
	raw, _ := json.Marshal(schema)
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "skill_invoke",
			Description: "Run a named skill synchronously as a child agent and return its result. Skills are reusable agent bundles (prompt + tools + model) loaded from .orchestra/skills/. Use this when a subtask matches an available skill's description.",
			Parameters:  raw,
		},
		Mutating: true,
	}
}

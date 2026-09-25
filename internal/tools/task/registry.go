package task

import (
	"encoding/json"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)

func ToolTask() llm.ToolDef {
	fallback := "Child agent (sync spawn+wait) for HEAVY/parallel work only. Prefer edit/write yourself for quick fixes. subagent_type: explore|ask|debug|architecture|verifier|general|worker|product|documentation|scout. Do NOT use for 1–3 known-file edits."
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
      "enum": ["explore", "ask", "debug", "architecture", "verifier", "general", "worker", "product", "documentation", "scout"],
      "description": "Child agent mode (default: explore)"
    },
    "task_type": { "type": "string", "description": "Orchestra routing key (orchestra_routing.yaml); defaults subagent_type/tier/model from the routing rule" },
    "tier": { "type": "string", "description": "Orchestra worker tier name (complex|focused|micro)" },
    "provider": { "type": "string", "description": "Optional named providers: map entry for child LLM" },
    "model": { "type": "string", "description": "Optional model id override for child LLM" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Wait timeout / child lifetime (default 600000 = 10 min; local models need minutes per step)" },
    "dept": { "type": "string", "description": "Department instance the child works for (backend, frontend@web): its scratchpad, playbook and inbox" },
    "depends_on": { "type": "array", "items": { "type": "string" }, "description": "task_ids or keys of this turn that must succeed first; their results are handed to the child" }
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
			Description: "Spawn a child asynchronously (rare). Prefer doing quick/concrete edits yourself with edit/write. Use only for parallel independent work; then task_wait. Batch: pass workorders[] (worker-only) to spawn one worker per WorkOrder in a single call; the runtime serializes WorkOrders with overlapping target_files and holds a WorkOrder until its depends_on[] (other WorkOrders' task_id) succeed.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["goal"] },
    { "required": ["prompt"] },
    { "required": ["workorders"] }
  ],
  "properties": {
    "goal": { "type": "string", "minLength": 1, "description": "Provide exactly one of goal/prompt/workorders" },
    "prompt": { "type": "string", "minLength": 1, "description": "Alias for goal" },
    "workorders": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "items": { "type": "object" },
      "description": "Batch of WorkOrder JSON objects (worker-only). Returns task_ids[] in spawn order; overlapping target_files are serialized automatically."
    },
    "subagent_type": {
      "type": "string",
      "enum": ["explore", "ask", "debug", "architecture", "verifier", "general", "worker", "product", "documentation", "scout"],
      "description": "Child agent mode (default: explore)"
    },
    "task_type": { "type": "string", "description": "Orchestra routing key (orchestra_routing.yaml); defaults subagent_type/tier/model from the routing rule" },
    "tier": { "type": "string", "description": "Orchestra worker tier name" },
    "provider": { "type": "string" },
    "model": { "type": "string" },
    "max_steps": { "type": "integer", "minimum": 1, "maximum": 12 },
    "timeout_ms": { "type": "integer", "minimum": 0, "description": "Child lifetime (default 600000 = 10 min); 0 also uses the default" },
    "dept": { "type": "string", "description": "Department instance the child works for (backend, frontend@web); WorkOrders without context.scratchpad default to it" },
    "key": { "type": "string", "description": "Name for depends_on of later spawns (a WorkOrder's task_id is its key)" },
    "depends_on": { "type": "array", "items": { "type": "string" }, "description": "task_ids or keys that must succeed before this child starts; a WorkOrder may carry its own depends_on" }
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
			Description: "Wait for a child task to finish and collect its result. task_ids[] waits for several; when two or more were workers that changed files, their edits are also built and tested together (integration). timeout_ms bounds the wait, not the task: a task still running then comes back as status still_running and keeps running — wait again later or task_cancel it.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "anyOf": [
    { "required": ["task_id"] },
    { "required": ["task_ids"] }
  ],
  "properties": {
    "task_id": { "type": "string", "minLength": 1 },
    "task_ids": { "type": "array", "minItems": 1, "maxItems": 16, "items": { "type": "string", "minLength": 1 } },
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
			Description: "Cancel a child task.",
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
			Description: "Report your findings to the parent agent. Call it once the investigation is done.",
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
			Description: "Switch to PLAN mode (read-only). Use it to analyse a task in depth before changing anything.",
			Parameters:  toolschema.MustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func ToolPlanExit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "plan_exit",
			Description: "Finish planning and request a switch to build mode. Call it only once the plan in {{PLAN_PATH}} is complete.",
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

// ToolSendMessage is the agency's conversation tool (agency-swarm's
// SendMessage): the recipient runs as the caller's child and its reply comes
// back as the result. Unlike `task`, the conversation between the two
// persists — a second message to the same agent continues the first.
func ToolSendMessage() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "send_message",
			Description: "Talk to another agent or department (see <available_agents>) and wait for its reply. The conversation persists: the next message to the same agent continues it. Use it to ask a peer, hand over a follow-up, or revise earlier work — not for new atomic edits (those are WorkOrders for workers).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["to", "message"],
  "properties": {
    "to": { "type": "string", "minLength": 1, "description": "Recipient: a department (backend, frontend@web), a custom agent, or a role from <available_agents>" },
    "message": { "type": "string", "minLength": 1, "description": "What you need from the recipient; it sees only this and the earlier turns of your conversation" },
    "role": { "type": "string", "description": "Built-in role a department runs as (default architecture — its Lead)" },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

// ToolAgentPost leaves a note for another agent without waiting — the async
// half of the agency (spec §3.6: Design → Frontend tokens, Frontend → Backend
// contract_change_request, Security → BE/FE findings). The runtime delivers
// it live when the recipient is running and from its inbox when it is next
// started.
func ToolAgentPost() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "agent_post",
			Description: "Leave a note for another agent or department without waiting for it: a running recipient sees it on its next step, an idle one when it is next started. contract_change_request goes to the artifact's owner and is copied to the orchestrator.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["to", "message"],
  "properties": {
    "to": { "type": "string", "minLength": 1, "description": "Recipient address: orchestrator, a department (backend, frontend@web), a custom agent, a role, or a running task_id" },
    "kind": { "type": "string", "enum": ["note", "question", "contract_change_request", "finding", "handoff"], "description": "Default note" },
    "message": { "type": "string", "minLength": 1, "maxLength": 4000 },
    "artifact": { "type": "string", "description": "contract_change_request: path of the contract artifact the delta applies to" }
  }
}`),
		},
	}
}

// ToolTaskBoard lists the turn's tasks — who is running, queued, waiting on a
// dependency, done or failed — so a Lead distributes work from the actual
// state rather than from what it remembers spawning.
func ToolTaskBoard() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "task_board",
			Description: "List every task of this turn with its agent, parent, status (queued|waiting_deps|running|done|error|timeout|cancelled), dependencies and elapsed time.",
			Parameters:  toolschema.MustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

// WithSubagentEnum returns def (task or task_spawn) with its subagent_type
// enum replaced by names. The input is not modified.
func WithSubagentEnum(def llm.ToolDef, names []string) llm.ToolDef {
	if len(names) == 0 || len(def.Function.Parameters) == 0 {
		return def
	}
	var schema map[string]any
	if err := json.Unmarshal(def.Function.Parameters, &schema); err != nil {
		return def
	}
	props, _ := schema["properties"].(map[string]any)
	st, _ := props["subagent_type"].(map[string]any)
	if st == nil {
		return def
	}
	enum := make([]any, len(names))
	for i, n := range names {
		enum[i] = n
	}
	st["enum"] = enum
	raw, err := json.Marshal(schema)
	if err != nil {
		return def
	}
	def.Function.Parameters = raw
	return def
}

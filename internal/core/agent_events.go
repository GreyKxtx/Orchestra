package core

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

// EventEnvelope tags streaming notifications with session/turn context for
// debugging and future UI correlation.
type EventEnvelope struct {
	SessionID string // set for session.message; omitted for one-shot agent.run
	TurnID    string // unique per agent.run or session.message invocation
}

// NewTurnID returns a sortable unique id for one agent turn.
func NewTurnID() string {
	return sessionfile.NewID()
}

func mergeEventEnvelope(base map[string]any, env EventEnvelope) map[string]any {
	if env.TurnID != "" {
		base["turn_id"] = env.TurnID
	}
	if env.SessionID != "" {
		base["session_id"] = env.SessionID
	}
	return base
}

// buildAgentOnEvent translates agent.AgentEvent to JSON-RPC notifications.
func buildAgentOnEvent(notify func(method string, params any), env EventEnvelope) func(agent.AgentEvent) {
	return func(ev agent.AgentEvent) {
		if ev.Stream.Kind == llm.StreamEventExecOutput {
			notify("exec/output_chunk", mergeEventEnvelope(map[string]any{
				"step":  ev.Step,
				"chunk": ev.Stream.Content,
			}, env))
			return
		}
		if ev.Stream.Kind == llm.StreamEventPendingOps {
			var data any
			if err := json.Unmarshal([]byte(ev.Stream.Content), &data); err == nil {
				notify("agent/event", mergeEventEnvelope(map[string]any{
					"step": ev.Step,
					"type": "pending_ops",
					"data": data,
				}, env))
				return
			}
		}
		if ev.Stream.Kind == llm.StreamEventError {
			msg := ""
			if ev.Stream.Err != nil {
				msg = ev.Stream.Err.Error()
			}
			notify("agent/event", mergeEventEnvelope(map[string]any{
				"step":    ev.Step,
				"type":    string(ev.Stream.Kind),
				"content": msg,
				"error":   msg,
			}, env))
			return
		}
		if ev.Stream.Kind == llm.StreamEventStepUsage {
			var data any
			if err := json.Unmarshal([]byte(ev.Stream.Content), &data); err == nil {
				notify("agent/event", mergeEventEnvelope(map[string]any{
					"step": ev.Step,
					"type": string(llm.StreamEventStepUsage),
					"data": data,
				}, env))
				return
			}
		}
		if ev.Stream.Kind == llm.StreamEventDone {
			if ev.Stream.Response != nil && ev.Stream.Response.Usage != nil {
				u := ev.Stream.Response.Usage
				notify("agent/event", mergeEventEnvelope(map[string]any{
					"step": ev.Step,
					"type": string(llm.StreamEventStepUsage),
					"data": map[string]any{
						"prompt_tokens":     u.PromptTokens,
						"completion_tokens": u.CompletionTokens,
						"total_tokens":      u.TotalTokens,
					},
				}, env))
			}
		}
		notify("agent/event", mergeEventEnvelope(map[string]any{
			"step":            ev.Step,
			"type":            string(ev.Stream.Kind),
			"content":         ev.Stream.Content,
			"tool_call_id":    ev.Stream.ToolCallID,
			"tool_call_name":  ev.Stream.ToolCallName,
			"tool_call_index": ev.Stream.ToolCallIndex,
			"args_delta":      ev.Stream.ArgsDelta,
			"diagnostics":     json.RawMessage(ev.Stream.Diagnostics),
		}, env))
	}
}

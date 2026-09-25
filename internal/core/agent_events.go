package core

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/wire"
)

// EventEnvelope tags streaming notifications with session/turn context for
// debugging and future UI correlation.
type EventEnvelope struct {
	SessionID string // set for session.message; omitted for one-shot agent.run
	TurnID    string // unique per agent.run or session.message invocation
}

// ChildScopeMeta tags agent/event notifications emitted by a child subagent.
type ChildScopeMeta struct {
	TaskID           string
	ParentToolCallID string
	SubagentType     string
}

// NewTurnID returns a sortable unique id for one agent turn.
func NewTurnID() string {
	return sessionfile.NewID()
}

// stamp puts the turn's envelope on ev, and the child's scope when the
// event is a subagent's: either child, or the task ev already names.
func (env EventEnvelope) stamp(ev wire.AgentEvent, child *ChildScopeMeta) wire.AgentEvent {
	ev.TurnID = env.TurnID
	ev.SessionID = env.SessionID
	if child != nil && child.TaskID != "" {
		ev.TaskID = child.TaskID
		if child.ParentToolCallID != "" {
			ev.ParentToolCallID = child.ParentToolCallID
		}
		if child.SubagentType != "" {
			ev.SubagentType = child.SubagentType
		}
	}
	if ev.TaskID != "" {
		ev.Scope = "child"
	}
	return ev
}

// buildAgentOnEvent translates agent.AgentEvent to JSON-RPC notifications.
func buildAgentOnEvent(notify func(method string, params any), env EventEnvelope) func(agent.AgentEvent) {
	return buildAgentOnEventWithChild(notify, env, nil)
}

// buildAgentOnEventWithChild is like buildAgentOnEvent but tags every notification
// with child scope metadata (task_id, parent_tool_call_id, subagent_type).
func buildAgentOnEventWithChild(notify func(method string, params any), env EventEnvelope, child *ChildScopeMeta) func(agent.AgentEvent) {
	emit := func(ev agent.AgentEvent) {
		emitAgentStreamEvent(notify, env, child, ev)
	}
	return wrapStreamDebounce(emit)
}

// childEventsFor is how the children of a call report to its client:
// their lifecycle stamped with the call's envelope, their stream scoped to
// the child. Zero when the call has no client to notify.
func childEventsFor(notify func(method string, params any), env EventEnvelope) app.ChildEvents {
	if notify == nil {
		return app.ChildEvents{}
	}
	return app.ChildEvents{
		Notify: func(ev wire.AgentEvent) {
			notify(wire.NotifyAgentEvent, env.stamp(ev, nil))
		},
		Stream: func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent) {
			return buildAgentOnEventWithChild(notify, env, &ChildScopeMeta{
				TaskID:           taskID,
				ParentToolCallID: parentToolCallID,
				SubagentType:     subagentType,
			})
		},
	}
}

// emitAgentStreamEvent sends ev as the wire's AgentEvent (or ExecOutputChunk).
// The payloads the agent carries as JSON text — pending ops, usage — go out
// typed; one that does not parse goes out as the text it was.
func emitAgentStreamEvent(notify func(method string, params any), env EventEnvelope, child *ChildScopeMeta, ev agent.AgentEvent) {
	if ev.Stream.Kind == llm.StreamEventExecOutput {
		chunk := wire.ExecOutputChunk{
			Step:      ev.Step,
			Chunk:     ev.Stream.Content,
			SessionID: env.SessionID,
			TurnID:    env.TurnID,
		}
		if child != nil && child.TaskID != "" {
			chunk.Scope = "child"
			chunk.TaskID = child.TaskID
			chunk.ParentToolCallID = child.ParentToolCallID
			chunk.SubagentType = child.SubagentType
		}
		notify(wire.NotifyExecOutputChunk, chunk)
		return
	}
	out := wire.AgentEvent{Step: ev.Step, Type: string(ev.Stream.Kind)}
	switch ev.Stream.Kind {
	case llm.StreamEventPendingOps:
		var ops wire.PendingOps
		if err := json.Unmarshal([]byte(ev.Stream.Content), &ops); err == nil {
			out.Data = ops
			notify(wire.NotifyAgentEvent, env.stamp(out, child))
			return
		}
	case llm.StreamEventStepUsage, llm.StreamEventContextEstimate:
		var usage wire.Usage
		if err := json.Unmarshal([]byte(ev.Stream.Content), &usage); err == nil {
			out.Data = usage
			notify(wire.NotifyAgentEvent, env.stamp(out, child))
			return
		}
	case llm.StreamEventError:
		if ev.Stream.Err != nil {
			out.Content = ev.Stream.Err.Error()
			out.Error = out.Content
		}
		notify(wire.NotifyAgentEvent, env.stamp(out, child))
		return
	}
	// StreamEventDone used to be translated into a step_usage notification
	// here, with the payload rebuilt field by field. The agent now emits a
	// proper StreamEventStepUsage on the streaming path too (see
	// Agent.emitStepUsage), so this synthesis would only duplicate it — and
	// duplicate it lossily, since it never learned about the cache counters.
	out.Content = ev.Stream.Content
	out.ToolCallID = ev.Stream.ToolCallID
	out.ToolCallName = ev.Stream.ToolCallName
	out.ToolCallIndex = ev.Stream.ToolCallIndex
	out.ArgsDelta = ev.Stream.ArgsDelta
	if len(ev.Stream.Diagnostics) > 0 {
		var diags []wire.ToolDiagnostic
		if err := json.Unmarshal(ev.Stream.Diagnostics, &diags); err == nil {
			out.Diagnostics = diags
		}
	}
	notify(wire.NotifyAgentEvent, env.stamp(out, child))
}

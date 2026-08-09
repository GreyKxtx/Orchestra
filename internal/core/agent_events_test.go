package core

import (
	"fmt"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

func TestBuildAgentOnEvent_Envelope(t *testing.T) {
	env := EventEnvelope{SessionID: "sess-1", TurnID: "turn-1"}
	var got []struct {
		method string
		params map[string]any
	}
	notify := func(method string, params any) {
		m, ok := params.(map[string]any)
		if !ok {
			t.Fatalf("params type %T", params)
		}
		got = append(got, struct {
			method string
			params map[string]any
		}{method, m})
	}
	onEvent := buildAgentOnEvent(notify, env)

	onEvent(agent.AgentEvent{
		Step: 1,
		Stream: llm.StreamEvent{
			Kind:    llm.StreamEventMessageDelta,
			Content: "hi",
		},
	})
	onEvent(agent.AgentEvent{
		Step: 2,
		Stream: llm.StreamEvent{
			Kind:    llm.StreamEventExecOutput,
			Content: "out",
		},
	})

	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	if got[0].method != "agent/event" || got[0].params["session_id"] != "sess-1" || got[0].params["turn_id"] != "turn-1" {
		t.Fatalf("agent/event envelope: %+v", got[0])
	}
	if got[1].method != "exec/output_chunk" || got[1].params["session_id"] != "sess-1" || got[1].params["turn_id"] != "turn-1" {
		t.Fatalf("exec/output_chunk envelope: %+v", got[1])
	}
}

func TestBuildAgentOnEvent_AgentRunOmitsSessionID(t *testing.T) {
	env := EventEnvelope{TurnID: "turn-only"}
	var params map[string]any
	onEvent := buildAgentOnEvent(func(_ string, p any) {
		params = p.(map[string]any)
	}, env)
	onEvent(agent.AgentEvent{
		Step: 1,
		Stream: llm.StreamEvent{
			Kind:    llm.StreamEventStepDone,
			Content: "final",
		},
	})
	if _, ok := params["session_id"]; ok {
		t.Fatalf("agent.run events must omit session_id, got %+v", params)
	}
	if params["turn_id"] != "turn-only" {
		t.Fatalf("turn_id: %+v", params)
	}
}

func TestBuildAgentOnEvent_StreamErrorCarriesMessage(t *testing.T) {
	env := EventEnvelope{TurnID: "turn-1"}
	var params map[string]any
	onEvent := buildAgentOnEvent(func(_ string, p any) {
		params = p.(map[string]any)
	}, env)
	onEvent(agent.AgentEvent{
		Step: 3,
		Stream: llm.StreamEvent{
			Kind: llm.StreamEventError,
			Err:  fmt.Errorf("SSE read error: connection reset"),
		},
	})
	if params["type"] != "error" {
		t.Fatalf("type=%v", params["type"])
	}
	if params["error"] != "SSE read error: connection reset" {
		t.Fatalf("error=%v", params["error"])
	}
}

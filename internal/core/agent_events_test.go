package core

import (
	"fmt"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/wire"
)

func TestBuildAgentOnEvent_Envelope(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "0")
	env := EventEnvelope{SessionID: "sess-1", TurnID: "turn-1"}
	var got []struct {
		method string
		params any
	}
	notify := func(method string, params any) {
		got = append(got, struct {
			method string
			params any
		}{method, params})
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
	ev, ok := got[0].params.(wire.AgentEvent)
	if got[0].method != wire.NotifyAgentEvent || !ok || ev.SessionID != "sess-1" || ev.TurnID != "turn-1" {
		t.Fatalf("agent/event envelope: %+v", got[0])
	}
	chunk, ok := got[1].params.(wire.ExecOutputChunk)
	if got[1].method != wire.NotifyExecOutputChunk || !ok || chunk.SessionID != "sess-1" || chunk.TurnID != "turn-1" {
		t.Fatalf("exec/output_chunk envelope: %+v", got[1])
	}
}

func TestBuildAgentOnEvent_AgentRunOmitsSessionID(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "0")
	env := EventEnvelope{TurnID: "turn-only"}
	var params wire.AgentEvent
	onEvent := buildAgentOnEvent(func(_ string, p any) {
		params = p.(wire.AgentEvent)
	}, env)
	onEvent(agent.AgentEvent{
		Step: 1,
		Stream: llm.StreamEvent{
			Kind:    llm.StreamEventStepDone,
			Content: "final",
		},
	})
	if params.SessionID != "" {
		t.Fatalf("agent.run events must omit session_id, got %+v", params)
	}
	if params.TurnID != "turn-only" {
		t.Fatalf("turn_id: %+v", params)
	}
}

func TestBuildAgentOnEvent_StreamErrorCarriesMessage(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "0")
	env := EventEnvelope{TurnID: "turn-1"}
	var params wire.AgentEvent
	onEvent := buildAgentOnEvent(func(_ string, p any) {
		params = p.(wire.AgentEvent)
	}, env)
	onEvent(agent.AgentEvent{
		Step: 3,
		Stream: llm.StreamEvent{
			Kind: llm.StreamEventError,
			Err:  fmt.Errorf("SSE read error: connection reset"),
		},
	})
	if params.Type != wire.EventError {
		t.Fatalf("type=%v", params.Type)
	}
	if params.Error != "SSE read error: connection reset" {
		t.Fatalf("error=%v", params.Error)
	}
}

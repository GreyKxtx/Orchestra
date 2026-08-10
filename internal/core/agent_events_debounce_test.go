package core

import (
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

func TestBuildAgentOnEvent_DebouncesMessageDelta(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "25")

	var mu sync.Mutex
	var got []map[string]any
	notify := func(_ string, params any) {
		mu.Lock()
		got = append(got, params.(map[string]any))
		mu.Unlock()
	}
	onEvent := buildAgentOnEvent(notify, EventEnvelope{TurnID: "t1"})

	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: llm.StreamEventMessageDelta, Content: "hel"}})
	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: llm.StreamEventMessageDelta, Content: "lo"}})

	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("expected no immediate notify, got %d", n)
	}

	time.Sleep(40 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("got %d notifications, want 1 batched", len(got))
	}
	if got[0]["content"] != "hello" {
		t.Fatalf("content=%q", got[0]["content"])
	}
}

func TestBuildAgentOnEvent_DebounceFlushesOnToolBoundary(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "500")

	var mu sync.Mutex
	var got []map[string]any
	onEvent := buildAgentOnEvent(func(_ string, params any) {
		mu.Lock()
		got = append(got, params.(map[string]any))
		mu.Unlock()
	}, EventEnvelope{TurnID: "t1"})

	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: llm.StreamEventMessageDelta, Content: "x"}})
	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{
		Kind:           llm.StreamEventToolCallStart,
		ToolCallID:     "c1",
		ToolCallName:   "read",
	}})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2 (flushed delta + tool start)", len(got))
	}
	if got[0]["type"] != "message_delta" || got[0]["content"] != "x" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1]["type"] != "tool_call_start" {
		t.Fatalf("second=%+v", got[1])
	}
}

func TestBuildAgentOnEvent_DebounceSeparateReasoning(t *testing.T) {
	t.Setenv("ORCH_STREAM_DEBOUNCE_MS", "25")

	var mu sync.Mutex
	var got []map[string]any
	onEvent := buildAgentOnEvent(func(_ string, params any) {
		mu.Lock()
		got = append(got, params.(map[string]any))
		mu.Unlock()
	}, EventEnvelope{TurnID: "t1"})

	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: llm.StreamEventReasoningDelta, Content: "a"}})
	onEvent(agent.AgentEvent{Step: 1, Stream: llm.StreamEvent{Kind: llm.StreamEventReasoningDelta, Content: "b"}})
	time.Sleep(40 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0]["type"] != "reasoning_delta" || got[0]["content"] != "ab" {
		t.Fatalf("got %+v", got)
	}
}

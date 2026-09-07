package core

import (
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// waitForEvents blocks until at least n events have been delivered, or fails.
//
// Sleeping for "debounce + a bit" is what made these tests flaky: a 15ms
// margin over a 25ms timer is nothing on a loaded CI runner under -race, and
// the failure looked like a broken debouncer ("got []") rather than a slow
// machine. Polling with a deadline far larger than the timer keeps the
// assertion — the flush does happen, and it coalesces — while removing the
// dependence on how fast the machine is.
func waitForEvents(t *testing.T, mu *sync.Mutex, got *[]map[string]any, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		have := len(*got)
		mu.Unlock()
		if have >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d events arrived before the deadline; the debounced flush never fired", have, n)
		}
		time.Sleep(time.Millisecond)
	}
}

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

	waitForEvents(t, &mu, &got, 1)

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

	// Nothing may be delivered before the timer fires: reasoning is debounced
	// on its own channel, not passed straight through.
	mu.Lock()
	immediate := len(got)
	mu.Unlock()
	if immediate != 0 {
		t.Fatalf("expected no immediate notify, got %d", immediate)
	}

	waitForEvents(t, &mu, &got, 1)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0]["type"] != "reasoning_delta" || got[0]["content"] != "ab" {
		t.Fatalf("got %+v", got)
	}
}

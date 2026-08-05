package rpcclient

import "testing"

func TestMergeCoalesce_MessageDeltas(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	// Fill the channel so subsequent sends coalesce.
	c.events <- Event{Kind: EventConnecting}

	if !c.mergeCoalesce(Event{Kind: EventMessageDelta, Content: "a"}) {
		t.Fatal("expected merge")
	}
	if !c.mergeCoalesce(Event{Kind: EventMessageDelta, Content: "b"}) {
		t.Fatal("expected merge")
	}
	c.coalesceMu.Lock()
	got := c.coalesce.Content
	c.coalesceMu.Unlock()
	if got != "ab" {
		t.Fatalf("content=%q", got)
	}
}

func TestMergeCoalesce_ToolCallDeltasSameID(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	c.events <- Event{Kind: EventConnecting}

	c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t1", ArgsDelta: `{"p":`})
	if !c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t1", ArgsDelta: `"x"}`}) {
		t.Fatal("expected merge")
	}
	c.coalesceMu.Lock()
	got := c.coalesce.ArgsDelta
	c.coalesceMu.Unlock()
	if got != `{"p":"x"}` {
		t.Fatalf("args=%q", got)
	}
}

func TestMergeCoalesce_NonDeltaRejected(t *testing.T) {
	c := &Client{}
	if c.mergeCoalesce(Event{Kind: EventAgentRunCompleted}) {
		t.Fatal("completed event should not coalesce")
	}
}

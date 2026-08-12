package rpcclient

import (
	"testing"
	"time"
)

func newTestClient(buf int) *Client {
	return &Client{
		events:          make(chan Event, buf),
		done:            make(chan struct{}),
		permWaiters:     map[int64]chan PermissionDecision{},
		questionWaiters: map[int64]chan []string{},
	}
}

func TestMergeCoalesce_MessageDeltas(t *testing.T) {
	c := newTestClient(1)
	// Fill the channel so subsequent sends coalesce.
	c.events <- Event{Kind: EventConnecting}

	if !c.mergeCoalesce(Event{Kind: EventMessageDelta, Content: "a"}) {
		t.Fatal("expected merge")
	}
	if !c.mergeCoalesce(Event{Kind: EventMessageDelta, Content: "b"}) {
		t.Fatal("expected merge")
	}
	c.coalesceMu.Lock()
	if len(c.coalesce) != 1 {
		c.coalesceMu.Unlock()
		t.Fatalf("queue len=%d", len(c.coalesce))
	}
	got := c.coalesce[0].Content
	c.coalesceMu.Unlock()
	if got != "ab" {
		t.Fatalf("content=%q", got)
	}
}

func TestMergeCoalesce_ToolCallDeltasSameID(t *testing.T) {
	c := newTestClient(1)
	c.events <- Event{Kind: EventConnecting}

	c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t1", ArgsDelta: `{"p":`})
	if !c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t1", ArgsDelta: `"x"}`}) {
		t.Fatal("expected merge")
	}
	c.coalesceMu.Lock()
	got := c.coalesce[0].ArgsDelta
	c.coalesceMu.Unlock()
	if got != `{"p":"x"}` {
		t.Fatalf("args=%q", got)
	}
}

// Alternating kinds must not overwrite each other: text after tool args goes
// into a new queue slot instead of clobbering the accumulated ArgsDelta.
func TestMergeCoalesce_AlternatingKindsPreserved(t *testing.T) {
	c := newTestClient(1)
	c.events <- Event{Kind: EventConnecting}

	c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t1", ArgsDelta: `{"a":1}`})
	c.mergeCoalesce(Event{Kind: EventMessageDelta, Content: "hello"})
	c.mergeCoalesce(Event{Kind: EventToolCallDelta, ToolCallID: "t2", ArgsDelta: `{"b":2}`})

	c.coalesceMu.Lock()
	defer c.coalesceMu.Unlock()
	if len(c.coalesce) != 3 {
		t.Fatalf("queue len=%d, want 3", len(c.coalesce))
	}
	if c.coalesce[0].ArgsDelta != `{"a":1}` || c.coalesce[1].Content != "hello" || c.coalesce[2].ArgsDelta != `{"b":2}` {
		t.Fatalf("queue=%+v", c.coalesce)
	}
}

func TestMergeCoalesce_NonDeltaRejected(t *testing.T) {
	c := newTestClient(0)
	if c.mergeCoalesce(Event{Kind: EventAgentRunCompleted}) {
		t.Fatal("completed event should not coalesce")
	}
}

// A critical event on a saturated channel must block until the consumer
// drains — it may never be dropped.
func TestSend_CriticalEventBlocksUntilDrained(t *testing.T) {
	c := newTestClient(1)
	c.events <- Event{Kind: EventConnecting} // saturate

	delivered := make(chan struct{})
	go func() {
		c.send(Event{Kind: EventAgentRunCompleted})
		close(delivered)
	}()

	select {
	case <-delivered:
		t.Fatal("send returned before the channel was drained")
	case <-time.After(50 * time.Millisecond):
	}

	<-c.events // drain the filler
	select {
	case ev := <-c.events:
		if ev.Kind != EventAgentRunCompleted {
			t.Fatalf("kind=%s", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("critical event was not delivered")
	}
	<-delivered
}

// Close must unblock a producer stuck on a full channel and must not panic
// with "send on closed channel".
func TestClose_UnblocksBlockedSender(t *testing.T) {
	c := newTestClient(1)
	c.events <- Event{Kind: EventConnecting} // saturate

	returned := make(chan struct{})
	go func() {
		c.send(Event{Kind: EventAgentRunCompleted})
		close(returned)
	}()
	time.Sleep(20 * time.Millisecond)

	go func() {
		c.sendMu.Lock() // simulate Close's drain barrier without a real process
		c.closed = true
		c.sendMu.Unlock()
	}()
	close(c.done)

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("blocked sender was not released by Close")
	}

	// After closed=true, further sends are no-ops (no panic on closed channel).
	c.send(Event{Kind: EventMessageDelta, Content: "late"})
}

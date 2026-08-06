package rpcclient

import (
	"encoding/json"
	"testing"
)

func TestHandleAgentEvent_Diagnostics(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	params, _ := json.Marshal(map[string]any{
		"type":           "tool_call_completed",
		"tool_call_id":   "t1",
		"tool_call_name": "edit",
		"content":        `{"path":"a.go"}`,
		"diagnostics": []map[string]any{
			{"severity": "error", "message": "bad", "start_line": 2, "start_col": 3},
		},
	})
	c.handleAgentEvent(params)
	ev := <-c.events
	if len(ev.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(ev.Diagnostics))
	}
	if ev.Diagnostics[0].Message != "bad" {
		t.Fatalf("message: %q", ev.Diagnostics[0].Message)
	}
}

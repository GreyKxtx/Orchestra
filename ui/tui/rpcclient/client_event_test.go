package rpcclient

import (
	"encoding/json"
	"testing"
)

func TestHandleAgentEvent_ErrorAndStepUsage(t *testing.T) {
	c := &Client{events: make(chan Event, 4)}

	errPayload, _ := json.Marshal(map[string]any{
		"type":  "error",
		"step":  2,
		"error": "context deadline exceeded",
	})
	c.handleAgentEvent(errPayload)

	usagePayload, _ := json.Marshal(map[string]any{
		"type": "step_usage",
		"step": 3,
		"data": map[string]any{
			"prompt_tokens":     18234,
			"completion_tokens": 512,
		},
	})
	c.handleAgentEvent(usagePayload)

	ev1 := <-c.events
	if ev1.Kind != EventError || ev1.Err != "context deadline exceeded" {
		t.Fatalf("error event: %+v", ev1)
	}
	ev2 := <-c.events
	if ev2.Kind != EventStepUsage || ev2.Usage == nil || ev2.Usage.PromptTokens != 18234 {
		t.Fatalf("step usage: %+v", ev2)
	}
}

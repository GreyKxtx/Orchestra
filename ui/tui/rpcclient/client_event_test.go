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

func TestHandleAgentEvent_ChildScopeAndLifecycle(t *testing.T) {
	c := &Client{events: make(chan Event, 8)}

	started, _ := json.Marshal(map[string]any{
		"type":          "child_started",
		"task_id":       "task_1",
		"subagent_type": "worker",
		"content":       "fix jwt",
	})
	c.handleAgentEvent(started)

	scoped, _ := json.Marshal(map[string]any{
		"type":           "tool_call_start",
		"scope":          "child",
		"task_id":        "task_1",
		"tool_call_id":   "c1",
		"tool_call_name": "read",
	})
	c.handleAgentEvent(scoped)

	queued, _ := json.Marshal(map[string]any{
		"type":        "child_queued",
		"task_id":     "task_2",
		"waiting_for": []string{"task_1"},
		"reason":      "overlapping target_files",
	})
	c.handleAgentEvent(queued)

	ev1 := <-c.events
	if ev1.Kind != EventChildStarted || ev1.TaskID != "task_1" {
		t.Fatalf("started: %+v", ev1)
	}
	ev2 := <-c.events
	if ev2.Kind != EventToolCallStart || ev2.Scope != "child" || ev2.TaskID != "task_1" {
		t.Fatalf("scoped: %+v", ev2)
	}
	ev3 := <-c.events
	if ev3.Kind != EventChildQueued || ev3.TaskID != "task_2" || ev3.WaitingReason != "overlapping target_files" {
		t.Fatalf("queued: %+v", ev3)
	}
}

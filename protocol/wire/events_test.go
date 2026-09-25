package wire

import (
	"encoding/json"
	"testing"
)

// The fields every client reads on every event are always on the wire;
// the ones a type does not use are absent, not zero.
func TestAgentEvent_JSONShape(t *testing.T) {
	b, err := json.Marshal(AgentEvent{Step: 3, Type: EventMessageDelta, Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"step", "type", "content", "tool_call_index"} {
		if _, ok := m[key]; !ok {
			t.Errorf("%q is always on the wire: %s", key, b)
		}
	}
	for _, key := range []string{"data", "error", "task_id", "scope", "diagnostics", "waiting_for", "depth"} {
		if _, ok := m[key]; ok {
			t.Errorf("%q is absent from a message_delta: %s", key, b)
		}
	}
}

func TestAgentEvent_DecodeData(t *testing.T) {
	want := PendingOps{Ops: []map[string]any{{"type": "file.write_atomic", "path": "a.go"}}, Diff: []FileDiff{{Path: "a.go", Before: "x", After: "y"}}, Applied: true}
	// Typed on the core side, a map after a trip through JSON, raw bytes and
	// the string form older cores sent: all decode the same.
	typed := AgentEvent{Type: EventPendingOps, Data: want}
	b, _ := json.Marshal(typed)
	var viaJSON AgentEvent
	if err := json.Unmarshal(b, &viaJSON); err != nil {
		t.Fatal(err)
	}
	rawData, _ := json.Marshal(want)
	for name, ev := range map[string]AgentEvent{
		"typed":  typed,
		"map":    viaJSON,
		"raw":    {Data: json.RawMessage(rawData)},
		"string": {Data: string(rawData)},
	} {
		var got PendingOps
		if err := ev.DecodeData(&got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got.Applied || len(got.Ops) != 1 || got.Ops[0]["path"] != "a.go" || len(got.Diff) != 1 || got.Diff[0] != want.Diff[0] {
			t.Fatalf("%s: got %+v", name, got)
		}
	}
	var none PendingOps
	if err := (AgentEvent{}).DecodeData(&none); err != ErrNoData {
		t.Fatalf("no data: %v", err)
	}
}

func TestEventTypes_AreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range EventTypes() {
		if seen[e] {
			t.Fatalf("duplicate event type %q", e)
		}
		seen[e] = true
	}
}

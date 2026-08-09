package agent

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestToolCallCompletedStreamEvent_Diagnostics(t *testing.T) {
	out, _ := json.Marshal(map[string]any{
		"path":     "main.go",
		"file_hash": "abc",
		"diagnostics": []map[string]any{
			{"severity": "error", "message": "undefined: x", "start_line": 1, "start_col": 1},
		},
	})
	ev := toolCallCompletedStreamEvent("edit", "call_1", out, nil)
	if ev.Kind != llm.StreamEventToolCallCompleted {
		t.Fatalf("kind: %s", ev.Kind)
	}
	if len(ev.Diagnostics) == 0 {
		t.Fatal("expected diagnostics on stream event")
	}
}

func TestToolCallCompletedStreamEvent_NoDiagnosticsForRead(t *testing.T) {
	out := []byte(`{"content":"hello"}`)
	ev := toolCallCompletedStreamEvent("read", "call_1", out, nil)
	if len(ev.Diagnostics) != 0 {
		t.Fatalf("read should not carry diagnostics, got %s", ev.Diagnostics)
	}
}

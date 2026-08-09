package llm

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLogger_LogStepClassified(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogToolCall("edit", 12)
	l.LogStepClassified(3, "validation_error", "", "invalid json")
	l.LogToolResult("edit", 4, 10, "")

	f, err := os.Open(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var events []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e LLMLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e.Event)
		if e.Event == "step.classified" {
			if e.Kind != "validation_error" || e.Step != 3 {
				t.Fatalf("classified entry: %+v", e)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"tool_call", "step.classified", "tool_result"}
	if len(events) != len(want) {
		t.Fatalf("events=%v want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d]=%q want %q", i, events[i], want[i])
		}
	}
}

func TestLogger_NilSafe(t *testing.T) {
	var l *Logger
	l.LogStepClassified(1, "tool_failed", "read", "boom")
	l.LogToolCall("x", 1)
	l.LogToolResult("x", 0, 0, "err")
}

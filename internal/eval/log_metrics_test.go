package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLLMLog_CountsEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm_log.jsonl")
	content := `{"event":"tool_call","tool_name":"read"}
{"event":"tool_result","tool_name":"read"}
{"event":"step.classified","kind":"validation_error","step":1}
{"event":"step.classified","kind":"tool_failed","step":2}
{"event":"step.classified","kind":"resolve_failed","step":3}
{"event":"llm_request"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseLLMLog(path)
	if err != nil {
		t.Fatalf("ParseLLMLog: %v", err)
	}
	if m.ToolCalls != 1 || m.ToolResults != 1 {
		t.Fatalf("tool events: calls=%d results=%d", m.ToolCalls, m.ToolResults)
	}
	if m.ValidationErrors != 1 || m.ResolveFailed != 1 || m.ClassifiedSteps != 3 {
		t.Fatalf("classified: invalid=%d resolve=%d steps=%d", m.ValidationErrors, m.ResolveFailed, m.ClassifiedSteps)
	}
}

func TestParseLLMLog_MissingFile(t *testing.T) {
	m, err := ParseLLMLog(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ValidationErrors != 0 {
		t.Fatalf("expected zero metrics for missing log")
	}
}

// The suite is the only place a tool is exercised end to end by a real model,
// so which tools it reached is the one measurable answer to "what does the
// eval cover". A count of calls cannot give it, and a list of registered
// tools answers a different question — what the model was offered.
func TestParseLLMLog_RecordsWhichToolsWereCalled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm_log.jsonl")
	content := `{"event":"tool_call","tool_name":"read"}
{"event":"tool_call","tool_name":"read"}
{"event":"tool_call","tool_name":"edit"}
{"event":"tool_result","tool_name":"edit"}
{"event":"tool_call"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseLLMLog(path)
	if err != nil {
		t.Fatalf("ParseLLMLog: %v", err)
	}
	if m.Tools["read"] != 2 {
		t.Errorf("read was called twice, got %d", m.Tools["read"])
	}
	if m.Tools["edit"] != 1 {
		t.Errorf("edit was called once, got %d; a tool_result must not count as a call",
			m.Tools["edit"])
	}
	if len(m.Tools) != 2 {
		t.Errorf("a tool_call with no name must not become an entry: %v", m.Tools)
	}
	if m.ToolCalls != 4 {
		t.Errorf("the total must still count every tool_call, named or not: %d", m.ToolCalls)
	}
}

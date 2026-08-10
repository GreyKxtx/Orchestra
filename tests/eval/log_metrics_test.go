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

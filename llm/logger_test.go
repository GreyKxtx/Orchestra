package llm

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_LogStepClassified(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogToolCall("edit", 12, `{"path":"a.go"}`)
	l.LogStepClassified(3, "validation_error", "", "invalid json")
	l.LogToolResult("edit", 4, 10, "", `{"file_hash":"sha256:ab"}`)

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
	l.LogToolCall("x", 1, "")
	l.LogToolResult("x", 0, 0, "err", "")
	l.LogMemoryNote("failed", "digest", "boom")
}

func TestLogger_LogMemoryNote(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogMemoryNote("written", "digest", "[session:s1] goal: fix; done: edit README.md")
	l.LogMemoryNote("failed", "model", "write agent.md: disk full; key sk-or-v1-abcdefghij1234")

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []LLMLogEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e LLMLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Memory used to report only to stderr, which nobody reads back. The log
	// is what the field-run analysis was built on; memory must show up there.
	if entries[0].Event != "memory.note" || entries[0].Kind != "written" || entries[0].Source != "digest" {
		t.Errorf("written entry: %+v", entries[0])
	}
	if entries[1].Kind != "failed" || !strings.Contains(entries[1].Detail, "disk full") {
		t.Errorf("failed entry: %+v", entries[1])
	}
	if strings.Contains(entries[1].Detail, "sk-or-v1-abcdefghij1234") {
		t.Error("memory.note detail must go through the secret sanitizer like every other entry")
	}
}

func TestLogger_LogMemoryInject(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogMemoryInject("orchestra=512B repo=0B global=0B total=512B/2048B")

	data, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var e LLMLogEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &e); err != nil {
		t.Fatal(err)
	}
	if e.Event != "memory.inject" {
		t.Errorf("event = %q, want memory.inject", e.Event)
	}
	if e.Detail != "orchestra=512B repo=0B global=0B total=512B/2048B" {
		t.Errorf("detail = %q", e.Detail)
	}
}

// TestSanitizeSecrets: key material in previews, error bodies and URLs must
// never survive into llm_log.jsonl (users attach these logs to bug reports).
func TestSanitizeSecrets(t *testing.T) {
	cases := []struct {
		in       string
		mustHide string
	}{
		{`Authorization: Bearer sk-or-v1-abcdef1234567890`, "sk-or-v1-abcdef1234567890"},
		{`{"api_key":"sk-live-XYZ"}`, "sk-live-XYZ"},
		{`{"authorization":"Basic dXNlcjpwYXNz"}`, "dXNlcjpwYXNz"},
		{`error: invalid key sk-ant-api03-verylongkeymaterial`, "sk-ant-api03-verylongkeymaterial"},
		{`https://generativelanguage.googleapis.com/v1?key=AIzaSyABCDEF1234567890abcdef`, "AIzaSyABCDEF1234567890abcdef"},
		{`https://api.example.com/v1?api_key=secret123&x=1`, "secret123"},
		{`token ghp_abcdefghijklmnopqrstuvwxyz123456`, "ghp_abcdefghijklmnopqrstuvwxyz123456"},
	}
	for _, c := range cases {
		out := sanitizeSecrets(c.in)
		if out == c.in {
			t.Errorf("not sanitized: %q", c.in)
		}
		if strings.Contains(out, c.mustHide) {
			t.Errorf("secret leaked: %q -> %q", c.in, out)
		}
	}
	// Plain text must pass through untouched.
	plain := "hello world, no secrets here"
	if got := sanitizeSecrets(plain); got != plain {
		t.Errorf("plain text mangled: %q", got)
	}
}

// A tool trace that records only sizes cannot be read back, and that is not a
// hypothetical: a delegation that changed nothing appeared in llm_log.jsonl as
// "the worker answered 112 bytes", and finding out what those bytes said took
// a scripted-child test and a separate direct run because the log could not
// say. llm_request and llm_response have carried previews all along; these put
// tool calls on the same footing.
func TestLogger_ToolEventsCarryWhatWasSaidNotJustHowLong(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogToolCall("task", 40, `{"subagent_type":"worker","prompt":"change width.go"}`)
	l.LogToolResult("task", 112, 8900, "", `{"status":"no_changes","detail":"nothing applied"}`)

	body, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "change width.go") {
		t.Errorf("the call's arguments are not in the trace, so a reader cannot tell what "+
			"was delegated:\n%s", got)
	}
	if !strings.Contains(got, "no_changes") {
		t.Errorf("the result is not in the trace, which is the whole reason the sizes were "+
			"not enough:\n%s", got)
	}
}

// Tool arguments carry whatever the model put in them, so the same
// sanitisation the request previews get has to apply here — otherwise the new
// field is a way for a key to reach a log file.
func TestLogger_ToolPreviewsAreStrippedOfKeyMaterial(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogToolCall("webfetch", 60, `{"url":"https://x.test/?api_key=sk-abcdefghijklmnop"}`)
	l.LogToolResult("webfetch", 20, 5, "", `Authorization: Bearer sk-ant-secret-value-here`)

	body, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, leaked := range []string{"sk-abcdefghijklmnop", "sk-ant-secret-value-here"} {
		if strings.Contains(got, leaked) {
			t.Errorf("key material reached the log through a tool preview: %q\n%s", leaked, got)
		}
	}
}

// A long result must not turn the trace into the payload it was summarising.
func TestLogger_ToolPreviewsAreCapped(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir)
	l.LogToolResult("read", 100000, 5, "", strings.Repeat("x", 100000))

	body, err := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 8*1024 {
		t.Errorf("one tool result wrote %d bytes of log; the preview is not capped", len(body))
	}
	if !strings.Contains(string(body), "truncated") {
		t.Errorf("a clipped preview must say it was clipped, or it reads as the whole answer:\n%s", body)
	}
}

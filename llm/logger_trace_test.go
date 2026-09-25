package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func readLogEntries(t *testing.T, dir string) []LLMLogEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []LLMLogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e LLMLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Parallel agents share one logger, so a line has to say whose it is.
func TestLogger_LinesCarryTheirRunAndTask(t *testing.T) {
	dir := t.TempDir()
	base := NewLogger(dir)
	tr := Trace{RunID: "run-1", TaskID: "task-2", ParentTaskID: "task-1", Depth: 2}
	ctx := WithTrace(context.Background(), tr)

	if got := TraceFrom(ctx); got != tr {
		t.Fatalf("TraceFrom = %+v, want %+v", got, tr)
	}
	if base.For(context.Background()) != base {
		t.Fatal("a ctx without a trace must leave the logger as it is")
	}
	var nilLogger *Logger
	if nilLogger.For(ctx) != nil || nilLogger.With(tr) != nil {
		t.Fatal("nil stays nil")
	}

	base.For(ctx).LogToolCall("read", 2, "{}")
	base.LogToolCall("grep", 2, "{}")

	entries := readLogEntries(t, dir)
	if len(entries) != 2 {
		t.Fatalf("want 2 lines, got %d", len(entries))
	}
	if entries[0].Trace != tr {
		t.Errorf("attributed line: %+v, want %+v", entries[0].Trace, tr)
	}
	if entries[1].Trace != (Trace{}) {
		t.Errorf("the base logger must not inherit a trace: %+v", entries[1].Trace)
	}

	// The fields are flat on the line, where a reader filters them.
	raw, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "llm_log.jsonl"))
	var flat map[string]any
	if err := json.Unmarshal(raw[:bytes.IndexByte(raw, '\n')], &flat); err != nil {
		t.Fatal(err)
	}
	if flat["run_id"] != "run-1" || flat["task_id"] != "task-2" || flat["parent_task_id"] != "task-1" || flat["depth"] != float64(2) {
		t.Errorf("trace fields must sit at the top level of the line: %v", flat)
	}
}

// The client logs the request and the answer under the trace of the call's
// ctx: that is how a model call is tied to the agent that made it.
func TestStream_RequestAndResponseAreAttributedToTheCallersTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAssistantSSE(w, "ok")
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	c := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m", MaxTokens: 64})
	c.SetLogger(NewLogger(dir))
	tr := Trace{RunID: "run-9", TaskID: "t-3", Depth: 1}
	resp, err := c.Complete(WithTrace(context.Background(), tr), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil || resp.Message.Content != "ok" {
		t.Fatalf("Complete: %v %+v", err, resp)
	}
	seen := map[string]bool{}
	for _, e := range readLogEntries(t, dir) {
		seen[e.Event] = true
		if e.Trace != tr {
			t.Errorf("%s line not attributed: %+v", e.Event, e.Trace)
		}
	}
	if !seen["llm_request"] || !seen["llm_response"] {
		t.Fatalf("want llm_request and llm_response, got %v", seen)
	}
}

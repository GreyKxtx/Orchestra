package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// Telemetry (audit 4.6): with an OTLP endpoint in the environment, every
// turn of the core leaves as a trace — the turn, its tool calls, and for
// a session turn the conversation it belongs to.

// otlpSink is a /v1/traces endpoint; it reads the spans back loosely.
type otlpSink struct {
	srv   *httptest.Server
	mu    sync.Mutex
	spans []map[string]any
}

func newOTLPSink(t *testing.T) *otlpSink {
	t.Helper()
	s := &otlpSink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			ResourceSpans []struct {
				ScopeSpans []struct {
					Spans []map[string]any `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("otlp sink: %v", err)
		}
		s.mu.Lock()
		for _, rs := range p.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				s.spans = append(s.spans, ss.Spans...)
			}
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *otlpSink) named(prefix string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, sp := range s.spans {
		if name, _ := sp["name"].(string); len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			out = append(out, sp)
		}
	}
	return out
}

func spanAttr(sp map[string]any, key string) string {
	attrs, _ := sp["attributes"].([]any)
	for _, a := range attrs {
		m, _ := a.(map[string]any)
		if m["key"] != key {
			continue
		}
		v, _ := m["value"].(map[string]any)
		if s, ok := v["stringValue"].(string); ok {
			return s
		}
		if s, ok := v["intValue"].(string); ok {
			return s
		}
	}
	return ""
}

// lsThenFinal lists the root, then finishes: one tool call per turn.
type lsThenFinal struct {
	mu    sync.Mutex
	calls int
}

func (l *lsThenFinal) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *lsThenFinal) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls%2 == 1 {
		return toolCall("ls", `{"path":"."}`), nil
	}
	return finalText("done"), nil
}

func TestTelemetry_ATurnLeavesAsATrace(t *testing.T) {
	sink := newOTLPSink(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", sink.srv.URL)
	t.Setenv("OTEL_SDK_DISABLED", "")
	c := newChatCore(t, &lsThenFinal{})
	if c.telemetry == nil {
		t.Fatal("an endpoint in the environment did not turn the exporter on")
	}
	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "list the root", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: started.SessionID, Content: "list it again", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	roots := sink.named("invoke_agent build")
	if len(roots) != 2 {
		t.Fatalf("got %d root spans, want one per turn", len(roots))
	}
	var withSession int
	for _, root := range roots {
		if spanAttr(root, "orchestra.stop_reason") != "completed" {
			t.Fatalf("root ended as %q, want completed: %+v", spanAttr(root, "orchestra.stop_reason"), root)
		}
		if spanAttr(root, "gen_ai.conversation.id") == started.SessionID {
			withSession++
		}
	}
	if withSession != 1 {
		t.Fatalf("%d root spans carry the session id, want the session turn's alone", withSession)
	}
	tools := sink.named("execute_tool ls")
	if len(tools) != 2 {
		t.Fatalf("got %d execute_tool ls spans, want one per turn", len(tools))
	}
	rootIDs := map[string]bool{}
	for _, root := range roots {
		id, _ := root["spanId"].(string)
		rootIDs[id] = true
	}
	for _, tool := range tools {
		parent, _ := tool["parentSpanId"].(string)
		if !rootIDs[parent] {
			t.Fatalf("a tool call of the turn is under %q, not its turn", parent)
		}
		if spanAttr(tool, "gen_ai.tool.name") != "ls" {
			t.Fatalf("tool span: %+v", tool)
		}
	}
}

func TestTelemetry_OffWithoutAnEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	c := newChatCore(t, &lsThenFinal{})
	if c.telemetry != nil {
		t.Fatal("a core with no endpoint built an exporter")
	}
	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "list the root", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
}

// spawnThenFinal: the root starts one explore child and finishes; the child
// answers at once.
type spawnThenFinal struct{}

func (spawnThenFinal) Plan(context.Context, string) (string, error) { return "{}", nil }

func (spawnThenFinal) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	steps := 0
	child := false
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
		if m.Role == llm.RoleUser && len(m.Content) >= 10 && m.Content[:10] == "CHILD-GOAL" {
			child = true
		}
	}
	if child {
		return toolCall("task_result", `{"content":"nothing to report"}`), nil
	}
	if steps == 0 {
		return toolCall("task", `{"subagent_type":"explore","goal":"CHILD-GOAL: look around"}`), nil
	}
	return finalText("done"), nil
}

func TestTelemetry_ASubagentIsASpanUnderItsTurn(t *testing.T) {
	sink := newOTLPSink(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", sink.srv.URL)
	t.Setenv("OTEL_SDK_DISABLED", "")
	c := newChatCore(t, spawnThenFinal{})
	if _, err := c.AgentRun(context.Background(), AgentRunParams{Query: "delegate", Mode: "build"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	roots := sink.named("invoke_agent build")
	if len(roots) != 1 {
		t.Fatalf("got %d root spans, want one", len(roots))
	}
	children := sink.named("invoke_agent explore")
	if len(children) != 1 {
		t.Fatalf("got %d explore spans, want the one child", len(children))
	}
	rootID, _ := roots[0]["spanId"].(string)
	if parent, _ := children[0]["parentSpanId"].(string); parent != rootID {
		t.Fatalf("the child is under %q, not its turn %q", parent, rootID)
	}
	if spanAttr(children[0], "orchestra.status") != "done" {
		t.Fatalf("child status: %+v", children[0])
	}
}

package telemetry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/wire"
)

// The exporter (audit 4.6): a turn — its agent, the subagents it started,
// their model calls and tool calls — leaves as one OTLP trace when the turn
// ends, named by the GenAI conventions.

// collector is a /v1/traces endpoint that keeps what it was sent.
type collector struct {
	srv      *httptest.Server
	mu       sync.Mutex
	payloads []otlpPayload
	headers  []http.Header
	status   int
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{status: http.StatusOK}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p otlpPayload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("collector: bad payload: %v", err)
		}
		c.mu.Lock()
		c.payloads = append(c.payloads, p)
		c.headers = append(c.headers, r.Header.Clone())
		status := c.status
		c.mu.Unlock()
		if r.URL.Path != "/v1/traces" {
			t.Errorf("collector: path %s, want /v1/traces", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *collector) received() []otlpPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]otlpPayload(nil), c.payloads...)
}

// spans flattens what the collector got.
func (c *collector) spans() []otlpSpan {
	var out []otlpSpan
	for _, p := range c.received() {
		for _, rs := range p.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				out = append(out, ss.Spans...)
			}
		}
	}
	return out
}

func newExporter(t *testing.T, c *collector) *Exporter {
	t.Helper()
	e, err := New(Config{Endpoint: c.srv.URL, ServiceVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Fatal("an endpoint was given, yet no exporter")
	}
	e.stderr = &bytes.Buffer{}
	return e
}

func attr(sp otlpSpan, key string) string {
	for _, a := range sp.Attributes {
		if a.Key != key {
			continue
		}
		switch {
		case a.Value.StringValue != nil:
			return *a.Value.StringValue
		case a.Value.IntValue != nil:
			return *a.Value.IntValue
		case a.Value.ArrayValue != nil:
			var parts []string
			for _, v := range a.Value.ArrayValue.Values {
				if v.StringValue != nil {
					parts = append(parts, *v.StringValue)
				}
			}
			return strings.Join(parts, ",")
		}
	}
	return ""
}

func spanNamed(t *testing.T, spans []otlpSpan, prefix string) otlpSpan {
	t.Helper()
	for _, sp := range spans {
		if strings.HasPrefix(sp.Name, prefix) {
			return sp
		}
	}
	names := make([]string, 0, len(spans))
	for _, sp := range spans {
		names = append(names, sp.Name)
	}
	t.Fatalf("no span named %q among %v", prefix, names)
	return otlpSpan{}
}

func TestExporter_ATurnIsOneTrace(t *testing.T) {
	col := newCollector(t)
	e := newExporter(t, col)
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	e.TurnStarted(TurnInfo{TurnID: "turn-1", SessionID: "sess-9", Mode: "build", Provider: "openai", Model: "gpt-x", StartedAt: at})
	root := llm.Trace{RunID: "turn-1"}
	e.Observe(llm.LLMLogEntry{Event: "llm_request", Trace: root, URL: "https://api.anthropic.com/v1/messages", Model: "claude-x", At: at})
	e.Observe(llm.LLMLogEntry{Event: "llm_response", Trace: root, Model: "claude-x", DurationMS: 1500, PromptTokens: 120, CompletionTokens: 30, CachedPromptTokens: 100, StopReason: "tool_use", At: at.Add(2 * time.Second)})
	e.Event(wire.AgentEvent{Type: wire.EventChildStarted, TurnID: "turn-1", TaskID: "task-1", SubagentType: "worker", Model: "small-x", Depth: 1})
	child := llm.Trace{RunID: "turn-1", TaskID: "task-1", Depth: 1}
	e.Observe(llm.LLMLogEntry{Event: "tool_result", Trace: child, ToolName: "edit", DurationMS: 40, At: at.Add(3 * time.Second)})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone, TurnID: "turn-1", TaskID: "task-1", SubagentType: "worker", Status: "done", Depth: 1})
	e.TurnEnded("turn-1", "completed", "")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	got := col.received()
	if len(got) != 1 {
		t.Fatalf("collector got %d payloads, want one per turn", len(got))
	}
	res := got[0].ResourceSpans[0].Resource
	if attr(otlpSpan{Attributes: res.Attributes}, "service.name") != "orchestra" {
		t.Fatalf("resource: %+v, want service.name orchestra", res.Attributes)
	}
	spans := col.spans()
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want root + chat + task + tool", len(spans))
	}
	rootSpan := spanNamed(t, spans, "invoke_agent build")
	chat := spanNamed(t, spans, "chat claude-x")
	task := spanNamed(t, spans, "invoke_agent worker")
	tool := spanNamed(t, spans, "execute_tool edit")
	for _, sp := range spans {
		if sp.TraceID != rootSpan.TraceID {
			t.Fatalf("span %s is in trace %s, the turn's is %s", sp.Name, sp.TraceID, rootSpan.TraceID)
		}
	}
	if rootSpan.ParentSpanID != "" || rootSpan.Status.Code != statusOK {
		t.Fatalf("root: parent %q status %+v", rootSpan.ParentSpanID, rootSpan.Status)
	}
	if attr(rootSpan, "gen_ai.conversation.id") != "sess-9" || attr(rootSpan, "orchestra.stop_reason") != "completed" || attr(rootSpan, "gen_ai.operation.name") != "invoke_agent" {
		t.Fatalf("root attributes: %+v", rootSpan.Attributes)
	}
	if rootSpan.StartTimeUnixNano != unixNano(at) {
		t.Fatalf("root starts at %s, want the turn's start %s", rootSpan.StartTimeUnixNano, unixNano(at))
	}
	if chat.ParentSpanID != rootSpan.SpanID {
		t.Fatal("the root's model call is not under the root span")
	}
	if chat.Kind != spanKindClient || attr(chat, "gen_ai.request.model") != "claude-x" || attr(chat, "gen_ai.provider.name") != "anthropic" || attr(chat, "server.address") != "api.anthropic.com" {
		t.Fatalf("chat attributes: kind %d %+v", chat.Kind, chat.Attributes)
	}
	if attr(chat, "gen_ai.usage.input_tokens") != "120" || attr(chat, "gen_ai.usage.output_tokens") != "30" || attr(chat, "gen_ai.usage.cache_read.input_tokens") != "100" || attr(chat, "gen_ai.response.finish_reasons") != "tool_use" {
		t.Fatalf("chat usage: %+v", chat.Attributes)
	}
	if chat.EndTimeUnixNano != unixNano(at.Add(2*time.Second)) || chat.StartTimeUnixNano != unixNano(at.Add(500*time.Millisecond)) {
		t.Fatalf("chat spans %s..%s, want the 1500 ms before its response", chat.StartTimeUnixNano, chat.EndTimeUnixNano)
	}
	if task.ParentSpanID != rootSpan.SpanID || attr(task, "gen_ai.agent.id") != "task-1" || attr(task, "gen_ai.request.model") != "small-x" || attr(task, "orchestra.depth") != "1" {
		t.Fatalf("task span: parent %q %+v", task.ParentSpanID, task.Attributes)
	}
	if tool.ParentSpanID != task.SpanID {
		t.Fatal("the worker's tool call is not under the worker's span")
	}
	if attr(tool, "gen_ai.tool.name") != "edit" || attr(tool, "orchestra.task_id") != "task-1" || tool.Status.Code != statusOK {
		t.Fatalf("tool span: %+v", tool.Attributes)
	}
}

func TestExporter_FailuresAreErrorsOnTheirSpans(t *testing.T) {
	col := newCollector(t)
	e := newExporter(t, col)
	e.TurnStarted(TurnInfo{TurnID: "turn-2", Mode: "build", Model: "gpt-x"})
	root := llm.Trace{RunID: "turn-2"}
	e.Observe(llm.LLMLogEntry{Event: "llm_request", Trace: root, URL: "https://api.openai.com/v1/chat/completions", Model: "gpt-x"})
	e.Observe(llm.LLMLogEntry{Event: "llm_error", Trace: root, HTTPCode: 429, ErrorBody: "rate limited\nmore", DurationMS: 10})
	e.Observe(llm.LLMLogEntry{Event: "tool_result", Trace: root, ToolName: "bash", ErrorStr: "exit 1", DurationMS: 5})
	e.Event(wire.AgentEvent{Type: wire.EventChildStarted, TurnID: "turn-2", TaskID: "t", SubagentType: "explore"})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone, TurnID: "turn-2", TaskID: "t", SubagentType: "explore", Status: "error", Error: "boom"})
	e.TurnEnded("turn-2", "", "the model gave up")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	spans := col.spans()
	chat := spanNamed(t, spans, "chat gpt-x")
	if chat.Status.Code != statusError || chat.Status.Message != "rate limited" || attr(chat, "error.type") != "429" || attr(chat, "http.response.status_code") != "429" {
		t.Fatalf("failed call: %+v %+v", chat.Status, chat.Attributes)
	}
	if attr(chat, "gen_ai.provider.name") != "openai" {
		t.Fatalf("provider of a failed call: %+v", chat.Attributes)
	}
	tool := spanNamed(t, spans, "execute_tool bash")
	if tool.Status.Code != statusError || attr(tool, "error.type") != "tool_error" {
		t.Fatalf("failed tool: %+v %+v", tool.Status, tool.Attributes)
	}
	task := spanNamed(t, spans, "invoke_agent explore")
	if task.Status.Code != statusError || task.Status.Message != "boom" || attr(task, "orchestra.status") != "error" {
		t.Fatalf("failed task: %+v %+v", task.Status, task.Attributes)
	}
	rootSpan := spanNamed(t, spans, "invoke_agent build")
	if rootSpan.Status.Code != statusError || rootSpan.Status.Message != "the model gave up" || attr(rootSpan, "orchestra.stop_reason") != "error" {
		t.Fatalf("failed turn: %+v %+v", rootSpan.Status, rootSpan.Attributes)
	}
}

func TestExporter_ACallLoggedBeforeItsTaskStartedLandsUnderTheTask(t *testing.T) {
	col := newCollector(t)
	e := newExporter(t, col)
	e.TurnStarted(TurnInfo{TurnID: "turn-3", Mode: "orchestra"})
	// The worker's first tool result reaches the logger before the
	// child_started notification reaches the exporter.
	e.Observe(llm.LLMLogEntry{Event: "tool_result", Trace: llm.Trace{RunID: "turn-3", TaskID: "w1", ParentTaskID: "lead", Depth: 2}, ToolName: "read"})
	e.Event(wire.AgentEvent{Type: wire.EventChildStarted, TurnID: "turn-3", TaskID: "lead", SubagentType: "lead", Depth: 1})
	e.Event(wire.AgentEvent{Type: wire.EventChildStarted, TurnID: "turn-3", TaskID: "w1", ParentTaskID: "lead", SubagentType: "worker", Depth: 2})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone, TurnID: "turn-3", TaskID: "w1", ParentTaskID: "lead", SubagentType: "worker", Status: "done", Depth: 2})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone, TurnID: "turn-3", TaskID: "lead", SubagentType: "lead", Status: "done", Depth: 1})
	e.TurnEnded("turn-3", "completed", "")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	spans := col.spans()
	lead := spanNamed(t, spans, "invoke_agent lead")
	worker := spanNamed(t, spans, "invoke_agent worker")
	tool := spanNamed(t, spans, "execute_tool read")
	if worker.ParentSpanID != lead.SpanID {
		t.Fatal("the worker is not under its Lead")
	}
	if tool.ParentSpanID != worker.SpanID {
		t.Fatalf("a call logged before its task started is under %s, not the task %s", tool.ParentSpanID, worker.SpanID)
	}
	if attr(tool, "orchestra.parent_task_id") != "lead" || attr(tool, "orchestra.depth") != "2" {
		t.Fatalf("tool scope: %+v", tool.Attributes)
	}
}

func TestExporter_LinesOutsideATurnAreNotSpans(t *testing.T) {
	col := newCollector(t)
	e := newExporter(t, col)
	e.Observe(llm.LLMLogEntry{Event: "llm_response", Model: "gpt-x", DurationMS: 5})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone, TaskID: "t"})
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if got := col.received(); len(got) != 0 {
		t.Fatalf("a line without a run id was exported: %+v", got)
	}
}

func TestExporter_CloseSendsTheTurnsStillOpen(t *testing.T) {
	col := newCollector(t)
	e := newExporter(t, col)
	e.TurnStarted(TurnInfo{TurnID: "turn-4", Mode: "ask"})
	e.Event(wire.AgentEvent{Type: wire.EventChildStarted, TurnID: "turn-4", TaskID: "t", SubagentType: "explore"})
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	spans := col.spans()
	root := spanNamed(t, spans, "invoke_agent ask")
	if attr(root, "orchestra.stop_reason") != "abandoned" {
		t.Fatalf("an unfinished turn closed as %q", attr(root, "orchestra.stop_reason"))
	}
	task := spanNamed(t, spans, "invoke_agent explore")
	if attr(task, "orchestra.status") != "unfinished" {
		t.Fatalf("an unfinished task closed as %q", attr(task, "orchestra.status"))
	}
	// Nothing after Close goes anywhere.
	e.TurnStarted(TurnInfo{TurnID: "turn-5"})
	e.TurnEnded("turn-5", "completed", "")
	if got := col.received(); len(got) != 1 {
		t.Fatalf("a turn after Close was exported: %d payloads", len(got))
	}
}

func TestExporter_AFailedExportIsReportedOnce(t *testing.T) {
	col := newCollector(t)
	col.status = http.StatusInternalServerError
	e := newExporter(t, col)
	stderr := &bytes.Buffer{}
	e.stderr = stderr
	for _, id := range []string{"a", "b"} {
		e.TurnStarted(TurnInfo{TurnID: id})
		e.TurnEnded(id, "completed", "")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(stderr.String(), "telemetry: export"); n != 1 {
		t.Fatalf("stderr reported %d failures, want one line:\n%s", n, stderr.String())
	}
	if !strings.Contains(stderr.String(), "500") {
		t.Fatalf("the report does not say what the collector answered:\n%s", stderr.String())
	}
}

func TestExporter_HeadersGoOnEveryExport(t *testing.T) {
	col := newCollector(t)
	e, err := New(Config{Endpoint: col.srv.URL + "/v1/traces", Headers: map[string]string{"Authorization": "Bearer t0k"}})
	if err != nil {
		t.Fatal(err)
	}
	e.TurnStarted(TurnInfo{TurnID: "h"})
	e.TurnEnded("h", "completed", "")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	col.mu.Lock()
	defer col.mu.Unlock()
	if len(col.headers) != 1 || col.headers[0].Get("Authorization") != "Bearer t0k" || col.headers[0].Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %+v", col.headers)
	}
}

func TestNew_NoEndpointNoExporter(t *testing.T) {
	e, err := New(Config{})
	if err != nil || e != nil {
		t.Fatalf("New without an endpoint: %v, %v", e, err)
	}
	// Every method is safe on the nil exporter a core without telemetry keeps.
	e.TurnStarted(TurnInfo{TurnID: "x"})
	e.Observe(llm.LLMLogEntry{Event: "llm_response"})
	e.Event(wire.AgentEvent{Type: wire.EventChildDone})
	e.TurnEnded("x", "", "")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Endpoint: "localhost:4318"}); err == nil {
		t.Fatal("an endpoint without a scheme was accepted")
	}
}

func TestTracesURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:4318":              "http://localhost:4318/v1/traces",
		"http://localhost:4318/":             "http://localhost:4318/v1/traces",
		"https://otel.example.com/v1/traces": "https://otel.example.com/v1/traces",
		"https://otel.example.com/otlp":      "https://otel.example.com/otlp/v1/traces",
	} {
		got, err := tracesURL(in)
		if err != nil || got != want {
			t.Errorf("tracesURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "localhost:4318", "ftp://x/y", "http://"} {
		if _, err := tracesURL(bad); err == nil {
			t.Errorf("tracesURL(%q) accepted", bad)
		}
	}
}

func TestConfig_FromEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20abc,X-Team=core")
	t.Setenv("OTEL_SERVICE_NAME", "orchestra-dev")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	c := Config{}.FromEnv()
	if c.Endpoint != "http://collector:4318" || c.Headers["Authorization"] != "Bearer abc" || c.Headers["X-Team"] != "core" || c.ServiceName != "orchestra-dev" {
		t.Fatalf("FromEnv: %+v", c)
	}
	// The config file wins over the environment where it says something.
	c = Config{Endpoint: "http://mine:4318", ServiceName: "mine"}.FromEnv()
	if c.Endpoint != "http://mine:4318" || c.ServiceName != "mine" {
		t.Fatalf("FromEnv over a set config: %+v", c)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4318/v1/traces")
	if c := (Config{}).FromEnv(); c.Endpoint != "http://traces:4318/v1/traces" {
		t.Fatalf("the traces endpoint does not win over the base one: %+v", c)
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if c := (Config{Endpoint: "http://mine:4318"}).FromEnv(); c.Enabled() {
		t.Fatal("OTEL_SDK_DISABLED=true left the exporter on")
	}
}

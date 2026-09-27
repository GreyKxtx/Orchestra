package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/wire"
)

// Config says where the traces go. Off until Endpoint is set, here or in
// the environment (FromEnv).
type Config struct {
	// Endpoint is the collector: the OTLP/HTTP base URL
	// (http://localhost:4318) or the traces URL itself (…/v1/traces).
	Endpoint string
	// Headers go on every export (an auth token for a hosted collector).
	Headers map[string]string
	// ServiceName is the resource's service.name; "orchestra" when empty.
	ServiceName string
	// ServiceVersion is the resource's service.version.
	ServiceVersion string
}

// FromEnv fills what c leaves empty from the standard variables:
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT,
// OTEL_EXPORTER_OTLP_TRACES_HEADERS or OTEL_EXPORTER_OTLP_HEADERS
// ("k=v,k2=v2"), OTEL_SERVICE_NAME. OTEL_SDK_DISABLED=true turns the
// exporter off whatever else is set.
func (c Config) FromEnv() Config {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		c.Endpoint = ""
		return c
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); v != "" {
			c.Endpoint = v
		} else if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); v != "" {
			c.Endpoint = v
		}
	}
	if len(c.Headers) == 0 {
		raw := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS")
		if strings.TrimSpace(raw) == "" {
			raw = os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
		}
		if h := parseHeaders(raw); len(h) > 0 {
			c.Headers = h
		}
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		c.ServiceName = strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	}
	return c
}

// Enabled says an endpoint is set.
func (c Config) Enabled() bool { return strings.TrimSpace(c.Endpoint) != "" }

// parseHeaders reads the OTEL_EXPORTER_OTLP_HEADERS form: comma-separated
// key=value pairs, values URL-encoded.
func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if dec, err := url.QueryUnescape(v); err == nil {
			v = dec
		}
		out[k] = v
	}
	return out
}

// tracesURL is the endpoint with /v1/traces on it, unless it is already
// the traces URL.
func tracesURL(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("telemetry endpoint %q: want http(s)://host[:port][/path]", endpoint)
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/v1/traces") {
		path += "/v1/traces"
	}
	u.Path = path
	return u.String(), nil
}

// maxOpenTurns bounds the turns kept in memory: a turn whose end the
// exporter never hears of (a process that dies mid-turn is gone with it,
// but a stray run id that never started) is exported as abandoned once
// enough newer ones have come.
const maxOpenTurns = 64

// closeWait bounds how long Close waits for the exports in flight.
const closeWait = 5 * time.Second

// Exporter turns what a turn does into spans and sends each turn's trace
// when the turn ends. It is fed from three places: the core says when a
// turn starts and ends (TurnStarted, TurnEnded), the logger hands it every
// model call and tool call (Observe, through llm.Logger.Observe), and the
// turn's notifications say when a subagent starts and finishes (Event). Nil
// is a valid receiver for every method: a core without an endpoint keeps a
// nil exporter and pays nothing.
type Exporter struct {
	cfg    Config
	url    string
	client *http.Client
	stderr io.Writer

	mu     sync.Mutex
	turns  map[string]*turn
	order  []string
	warned bool
	closed bool

	wg sync.WaitGroup
}

// New returns an exporter for cfg, or nil when it names no endpoint. An
// endpoint that is not an http(s) URL is an error.
func New(cfg Config) (*Exporter, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	u, err := tracesURL(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ServiceName) == "" {
		cfg.ServiceName = "orchestra"
	}
	return &Exporter{
		cfg:    cfg,
		url:    u,
		client: &http.Client{Timeout: postTimeout},
		stderr: os.Stderr,
		turns:  map[string]*turn{},
	}, nil
}

// URL is where the traces go.
func (e *Exporter) URL() string {
	if e == nil {
		return ""
	}
	return e.url
}

// TurnInfo is what the core knows about a turn when it starts.
type TurnInfo struct {
	TurnID    string
	SessionID string
	// Mode is the effective mode; Provider and Model the turn's client.
	Mode      string
	Provider  string
	Model     string
	StartedAt time.Time
}

// turn is one trace under construction.
type turn struct {
	info    TurnInfo
	traceID string
	rootID  string
	started time.Time
	spans   []otlpSpan
	// open are the subagents that started and have not finished.
	open map[string]*openTask
	// endpoints and models remember, per task ("" for the root), what the
	// last llm_request named, for the llm_response and llm_error lines
	// that follow it.
	endpoints map[string]string
	models    map[string]string
}

type openTask struct {
	started      time.Time
	kind         string
	model        string
	depth        int
	parentTaskID string
}

// TurnStarted registers a turn. A turn the exporter first hears of through
// a log line or an event exists already, with what that line said.
func (e *Exporter) TurnStarted(info TurnInfo) {
	if e == nil || info.TurnID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.turnLocked(info.TurnID)
	if info.StartedAt.IsZero() {
		info.StartedAt = time.Now()
	}
	t.info = info
	t.started = info.StartedAt
}

// turnLocked returns the turn for id, creating it — and, past the cap,
// exporting the oldest as abandoned.
func (e *Exporter) turnLocked(id string) *turn {
	if t := e.turns[id]; t != nil {
		return t
	}
	for len(e.order) >= maxOpenTurns {
		oldest := e.order[0]
		e.order = e.order[1:]
		if old := e.turns[oldest]; old != nil {
			delete(e.turns, oldest)
			e.finishLocked(old, "abandoned", "the exporter never heard the turn end")
		}
	}
	t := &turn{
		info:      TurnInfo{TurnID: id},
		traceID:   hexHash(16, "orchestra:trace:", id),
		rootID:    hexHash(8, "orchestra:turn:", id),
		started:   time.Now(),
		open:      map[string]*openTask{},
		endpoints: map[string]string{},
		models:    map[string]string{},
	}
	e.turns[id] = t
	e.order = append(e.order, id)
	return t
}

// TurnEnded closes the turn's root span with how it ended — the agent's
// stop reason ("completed", "partial", "max_steps"), or "error" with the
// error's text — and sends the trace.
func (e *Exporter) TurnEnded(turnID, status, errText string) {
	if e == nil || turnID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.turns[turnID]
	if t == nil {
		return
	}
	delete(e.turns, turnID)
	for i, id := range e.order {
		if id == turnID {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	e.finishLocked(t, status, errText)
}

// finishLocked ends the turn's open subagents and root span and hands the
// trace to a goroutine to send.
func (e *Exporter) finishLocked(t *turn, status, errText string) {
	now := time.Now()
	for taskID, ot := range t.open {
		t.spans = append(t.spans, e.taskSpan(t, taskID, ot, now, "unfinished", "the turn ended before the subagent did"))
	}
	t.open = map[string]*openTask{}
	if status == "" {
		status = "completed"
		if errText != "" {
			status = "error"
		}
	}
	root := otlpSpan{
		TraceID:           t.traceID,
		SpanID:            t.rootID,
		Name:              "invoke_agent " + orDefault(t.info.Mode, "agent"),
		Kind:              spanKindInternal,
		StartTimeUnixNano: unixNano(t.started),
		EndTimeUnixNano:   unixNano(now),
		Attributes: []otlpAttribute{
			attrString("gen_ai.operation.name", "invoke_agent"),
			attrString("gen_ai.agent.name", orDefault(t.info.Mode, "agent")),
			attrString("orchestra.turn_id", t.info.TurnID),
			attrString("orchestra.stop_reason", status),
		},
		Status: otlpStatus{Code: statusOK},
	}
	if t.info.SessionID != "" {
		root.Attributes = append(root.Attributes, attrString("gen_ai.conversation.id", t.info.SessionID))
	}
	if t.info.Model != "" {
		root.Attributes = append(root.Attributes, attrString("gen_ai.request.model", t.info.Model))
	}
	if t.info.Provider != "" {
		root.Attributes = append(root.Attributes, attrString("gen_ai.provider.name", t.info.Provider))
	}
	if errText != "" {
		root.Status = otlpStatus{Code: statusError, Message: errText}
		root.Attributes = append(root.Attributes, attrString("error.type", "agent_error"))
	}
	spans := append(t.spans, root)
	payload := e.payload(spans)
	if e.closed {
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		if err := post(context.Background(), e.client, e.url, e.cfg.Headers, payload); err != nil {
			e.warn(err)
		}
	}()
}

// warn reports the first export failure; the rest are the same story.
func (e *Exporter) warn(err error) {
	e.mu.Lock()
	first := !e.warned
	e.warned = true
	e.mu.Unlock()
	if first && e.stderr != nil {
		fmt.Fprintf(e.stderr, "orchestra: telemetry: export to %s failed: %v (further failures are not reported)\n", e.url, err)
	}
}

func (e *Exporter) payload(spans []otlpSpan) otlpPayload {
	res := otlpResource{Attributes: []otlpAttribute{attrString("service.name", e.cfg.ServiceName)}}
	if e.cfg.ServiceVersion != "" {
		res.Attributes = append(res.Attributes, attrString("service.version", e.cfg.ServiceVersion))
	}
	return otlpPayload{ResourceSpans: []otlpResourceSpans{{
		Resource: res,
		ScopeSpans: []otlpScopeSpan{{
			Scope: otlpScope{Name: "github.com/orchestra/orchestra/internal/telemetry", Version: e.cfg.ServiceVersion},
			Spans: spans,
		}},
	}}}
}

// Observe is the logger's hook: a model call becomes a "chat <model>" span
// when its response or error is logged, a tool call an "execute_tool
// <name>" span when its result is. Each is built from the one line that
// carries its duration, so parallel calls need no pairing. A line outside
// a turn (no run_id: llm-ping, a one-off client) is not a span.
func (e *Exporter) Observe(entry llm.LLMLogEntry) {
	if e == nil || entry.RunID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.turnLocked(entry.RunID)
	switch entry.Event {
	case "llm_request":
		if entry.URL != "" {
			t.endpoints[entry.TaskID] = entry.URL
		}
		if entry.Model != "" {
			t.models[entry.TaskID] = entry.Model
		}
	case "llm_response":
		t.spans = append(t.spans, e.chatSpan(t, entry, false))
	case "llm_error":
		t.spans = append(t.spans, e.chatSpan(t, entry, true))
	case "tool_result":
		t.spans = append(t.spans, e.toolSpan(t, entry))
	}
}

// chatSpan is one model call, from its response or error line.
func (e *Exporter) chatSpan(t *turn, entry llm.LLMLogEntry, failed bool) otlpSpan {
	end := entry.At
	if end.IsZero() {
		end = time.Now()
	}
	start := end.Add(-time.Duration(entry.DurationMS) * time.Millisecond)
	model := entry.Model
	if model == "" {
		model = t.models[entry.TaskID]
	}
	if model == "" {
		model = t.info.Model
	}
	sp := otlpSpan{
		TraceID:           t.traceID,
		SpanID:            randomID(),
		ParentSpanID:      e.parentOf(t, entry.Trace),
		Name:              "chat " + orDefault(model, "model"),
		Kind:              spanKindClient,
		StartTimeUnixNano: unixNano(start),
		EndTimeUnixNano:   unixNano(end),
		Attributes: append(scopeAttrs(t, entry.Trace),
			attrString("gen_ai.operation.name", "chat"),
		),
		Status: otlpStatus{Code: statusOK},
	}
	if model != "" {
		sp.Attributes = append(sp.Attributes, attrString("gen_ai.request.model", model))
	}
	if ep := t.endpoints[entry.TaskID]; ep != "" {
		sp.Attributes = append(sp.Attributes, endpointAttrs(ep)...)
	} else if t.info.Provider != "" {
		sp.Attributes = append(sp.Attributes, attrString("gen_ai.provider.name", t.info.Provider))
	}
	if failed {
		sp.Status = otlpStatus{Code: statusError, Message: firstLine(entry.ErrorBody)}
		if entry.HTTPCode > 0 {
			sp.Attributes = append(sp.Attributes,
				attrInt("http.response.status_code", int64(entry.HTTPCode)),
				attrString("error.type", strconv.Itoa(entry.HTTPCode)))
		} else {
			sp.Attributes = append(sp.Attributes, attrString("error.type", "transport"))
		}
		return sp
	}
	if entry.PromptTokens > 0 || entry.CompletionTokens > 0 {
		sp.Attributes = append(sp.Attributes,
			attrInt("gen_ai.usage.input_tokens", int64(entry.PromptTokens)),
			attrInt("gen_ai.usage.output_tokens", int64(entry.CompletionTokens)))
	}
	if entry.CachedPromptTokens > 0 {
		sp.Attributes = append(sp.Attributes, attrInt("gen_ai.usage.cache_read.input_tokens", int64(entry.CachedPromptTokens)))
	}
	if entry.CacheWriteTokens > 0 {
		sp.Attributes = append(sp.Attributes, attrInt("gen_ai.usage.cache_creation.input_tokens", int64(entry.CacheWriteTokens)))
	}
	if entry.StopReason != "" {
		sp.Attributes = append(sp.Attributes, attrStrings("gen_ai.response.finish_reasons", []string{entry.StopReason}))
	}
	return sp
}

// toolSpan is one tool call, from its result line.
func (e *Exporter) toolSpan(t *turn, entry llm.LLMLogEntry) otlpSpan {
	end := entry.At
	if end.IsZero() {
		end = time.Now()
	}
	start := end.Add(-time.Duration(entry.DurationMS) * time.Millisecond)
	sp := otlpSpan{
		TraceID:           t.traceID,
		SpanID:            randomID(),
		ParentSpanID:      e.parentOf(t, entry.Trace),
		Name:              "execute_tool " + orDefault(entry.ToolName, "tool"),
		Kind:              spanKindInternal,
		StartTimeUnixNano: unixNano(start),
		EndTimeUnixNano:   unixNano(end),
		Attributes: append(scopeAttrs(t, entry.Trace),
			attrString("gen_ai.operation.name", "execute_tool"),
			attrString("gen_ai.tool.name", entry.ToolName),
		),
		Status: otlpStatus{Code: statusOK},
	}
	if entry.ErrorStr != "" {
		sp.Status = otlpStatus{Code: statusError, Message: firstLine(entry.ErrorStr)}
		sp.Attributes = append(sp.Attributes, attrString("error.type", "tool_error"))
	}
	return sp
}

// Event is the turn's notification stream: a subagent's start opens its
// span, its end closes it. Everything else is the client's business.
func (e *Exporter) Event(ev wire.AgentEvent) {
	if e == nil || ev.TurnID == "" {
		return
	}
	switch ev.Type {
	case wire.EventChildStarted, wire.EventChildDone:
	default:
		return
	}
	if ev.TaskID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.turnLocked(ev.TurnID)
	switch ev.Type {
	case wire.EventChildStarted:
		t.open[ev.TaskID] = &openTask{
			started:      time.Now(),
			kind:         ev.SubagentType,
			model:        ev.Model,
			depth:        ev.Depth,
			parentTaskID: ev.ParentTaskID,
		}
	case wire.EventChildDone:
		ot := t.open[ev.TaskID]
		delete(t.open, ev.TaskID)
		if ot == nil {
			ot = &openTask{started: time.Now(), kind: ev.SubagentType, depth: ev.Depth, parentTaskID: ev.ParentTaskID}
		}
		if ot.kind == "" {
			ot.kind = ev.SubagentType
		}
		t.spans = append(t.spans, e.taskSpan(t, ev.TaskID, ot, time.Now(), ev.Status, ev.Error))
	}
}

// taskSpan is one subagent: "invoke_agent <kind>" under its parent task or
// the turn.
func (e *Exporter) taskSpan(t *turn, taskID string, ot *openTask, end time.Time, status, errText string) otlpSpan {
	parent := t.rootID
	if ot.parentTaskID != "" {
		parent = taskSpanID(t.info.TurnID, ot.parentTaskID)
	}
	sp := otlpSpan{
		TraceID:           t.traceID,
		SpanID:            taskSpanID(t.info.TurnID, taskID),
		ParentSpanID:      parent,
		Name:              "invoke_agent " + orDefault(ot.kind, "subagent"),
		Kind:              spanKindInternal,
		StartTimeUnixNano: unixNano(ot.started),
		EndTimeUnixNano:   unixNano(end),
		Attributes: []otlpAttribute{
			attrString("gen_ai.operation.name", "invoke_agent"),
			attrString("gen_ai.agent.name", orDefault(ot.kind, "subagent")),
			attrString("gen_ai.agent.id", taskID),
			attrString("orchestra.turn_id", t.info.TurnID),
			attrString("orchestra.task_id", taskID),
			attrInt("orchestra.depth", int64(ot.depth)),
			attrString("orchestra.status", orDefault(status, "done")),
		},
		Status: otlpStatus{Code: statusOK},
	}
	if ot.parentTaskID != "" {
		sp.Attributes = append(sp.Attributes, attrString("orchestra.parent_task_id", ot.parentTaskID))
	}
	if ot.model != "" {
		sp.Attributes = append(sp.Attributes, attrString("gen_ai.request.model", ot.model))
	}
	switch status {
	case "", "done":
	default:
		sp.Status = otlpStatus{Code: statusError, Message: firstLine(orDefault(errText, status))}
		sp.Attributes = append(sp.Attributes, attrString("error.type", status))
	}
	return sp
}

// parentOf is the span a log line's call belongs under: its task's span,
// or the turn's root.
func (e *Exporter) parentOf(t *turn, tr llm.Trace) string {
	if tr.TaskID == "" {
		return t.rootID
	}
	return taskSpanID(t.info.TurnID, tr.TaskID)
}

// scopeAttrs place a call in the delegation tree.
func scopeAttrs(t *turn, tr llm.Trace) []otlpAttribute {
	out := []otlpAttribute{attrString("orchestra.turn_id", t.info.TurnID)}
	if tr.TaskID != "" {
		out = append(out, attrString("orchestra.task_id", tr.TaskID), attrInt("orchestra.depth", int64(tr.Depth)))
	}
	if tr.ParentTaskID != "" {
		out = append(out, attrString("orchestra.parent_task_id", tr.ParentTaskID))
	}
	return out
}

// endpointAttrs name the provider behind a request URL: server.address and
// server.port, and gen_ai.provider.name from the host where it is known.
func endpointAttrs(rawURL string) []otlpAttribute {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil
	}
	out := []otlpAttribute{attrString("server.address", u.Hostname())}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			out = append(out, attrInt("server.port", int64(n)))
		}
	}
	return append(out, attrString("gen_ai.provider.name", providerOfHost(u.Hostname())))
}

// providerOfHost maps a host to the provider names the conventions use.
func providerOfHost(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.HasSuffix(h, "anthropic.com"):
		return "anthropic"
	case strings.HasSuffix(h, "openai.com"):
		return "openai"
	case strings.HasSuffix(h, "openai.azure.com"):
		return "azure.ai.openai"
	case strings.HasSuffix(h, "openrouter.ai"):
		return "openrouter"
	case strings.HasSuffix(h, "googleapis.com"):
		return "gcp.gemini"
	case strings.HasSuffix(h, "amazonaws.com"):
		return "aws.bedrock"
	case strings.HasSuffix(h, "mistral.ai"):
		return "mistral_ai"
	case strings.HasSuffix(h, "groq.com"):
		return "groq"
	case strings.HasSuffix(h, "deepseek.com"):
		return "deepseek"
	case strings.HasSuffix(h, "x.ai"):
		return "x_ai"
	}
	return h
}

// Close waits for the exports in flight, up to closeWait; a turn still open
// is sent as abandoned first.
func (e *Exporter) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	for _, id := range e.order {
		if t := e.turns[id]; t != nil {
			e.finishLocked(t, "abandoned", "the core closed before the turn ended")
		}
	}
	e.turns = map[string]*turn{}
	e.order = nil
	e.closed = true
	e.mu.Unlock()
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(closeWait):
		return fmt.Errorf("telemetry: exports still in flight after %s", closeWait)
	}
}

// taskSpanID is the same for every mention of a task in its turn, so a
// call logged under a task lands under its span whether or not the
// exporter has heard the task start yet.
func taskSpanID(turnID, taskID string) string {
	return hexHash(8, "orchestra:task:", turnID+"/"+taskID)
}

func hexHash(n int, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:n])
}

func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hexHash(8, "orchestra:span:", strconv.FormatInt(time.Now().UnixNano(), 10))
	}
	return hex.EncodeToString(b[:])
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

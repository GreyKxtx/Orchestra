package rpcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/protocol/jsonrpc"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

// Config configures the spawn + initialize handshake.
type Config struct {
	Binary        string // path to the orchestra executable
	WorkspaceRoot string // project root for `--workspace-root`
	ProjectID     string // optional, passed to initialize
}

// Client wraps a running `orchestra core` subprocess.
type Client struct {
	cfg Config

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	rpc *jsonrpc.Client

	events chan Event
	// done is closed on Close. It unblocks any producer goroutine that is
	// blocked delivering a critical event into the (full) events channel,
	// so Close can safely wait for in-flight sends before closing events.
	done chan struct{}

	closeOnce sync.Once
	// sendMu serializes send() against Close(): senders hold RLock for the
	// whole delivery, Close takes Lock after closing done — guaranteeing no
	// goroutine can be mid-send when the events channel is closed (which
	// would panic with "send on closed channel").
	sendMu sync.RWMutex
	closed bool

	coalesceMu sync.Mutex
	coalesce   []Event // FIFO of merged deltas when events channel is saturated

	// reqMu guards correlation state for server-initiated requests
	// (permission/request, question/ask). Each in-flight request gets its
	// own id + response channel so a stale answer can never be delivered
	// to a different request.
	reqMu           sync.Mutex
	reqSeq          int64
	permWaiters     map[int64]chan PermissionDecision
	questionWaiters map[int64]chan []string
}

// PermissionDecision is the TUI answer to permission/request.
type PermissionDecision struct {
	Approved bool
	Always   bool
}

// Spawn starts the orchestra core subprocess and runs the initialize handshake.
// On any error during spawn or initialize, the subprocess is killed and the
// error is returned.
func Spawn(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Binary == "" {
		return nil, fmt.Errorf("rpcclient: Config.Binary is empty")
	}
	if cfg.WorkspaceRoot == "" {
		return nil, fmt.Errorf("rpcclient: Config.WorkspaceRoot is empty")
	}

	// Deliberately NOT exec.CommandContext (resilience audit P3): the core
	// must be able to outlive the TUI to finish an in-flight agent turn.
	// Closing stdin (Close) makes the core's Serve loop return; the core
	// finishes the running turn, persists session snapshots and exits on
	// its own. A ctx-bound command would hard-kill it mid-write instead.
	cmd := exec.Command(cfg.Binary, "core", "--workspace-root", cfg.WorkspaceRoot)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rpcclient: start: %w", err)
	}

	c := &Client{
		cfg:             cfg,
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
		rpc:             jsonrpc.NewClient(stdout, stdin),
		events:          make(chan Event, 64),
		done:            make(chan struct{}),
		permWaiters:     map[int64]chan PermissionDecision{},
		questionWaiters: map[int64]chan []string{},
	}

	// Drain stderr to avoid pipe blocking. A panic here must never crash the
	// host process (the terminal would be left in raw/alt-screen mode).
	go func() {
		defer func() { _ = recover() }()
		buf := make([]byte, 4096)
		for {
			_, err := stderr.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	c.rpc.SetNotificationHandler(c.handleNotification)
	c.rpc.SetRequestHandler(c.handleRequest)

	c.send(Event{Kind: EventConnecting})
	initParams := map[string]any{
		"project_root":     cfg.WorkspaceRoot,
		"project_id":       cfg.ProjectID,
		"protocol_version": protocol.ProtocolVersion,
		"ops_version":      protocol.OpsVersion,
		"tools_version":    protocol.ToolsVersion,
	}
	var initResult struct {
		Health struct {
			LSPStatus string `json:"lsp_status"`
		} `json:"health"`
	}
	if err := c.rpc.Call(ctx, "initialize", initParams, &initResult); err != nil {
		// Handshake failure: nothing valuable is running yet — hard-kill so
		// a broken core cannot linger in the background.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = c.Close()
		return nil, fmt.Errorf("rpcclient: initialize: %w", err)
	}
	c.send(Event{Kind: EventInitialized, LSPStatus: initResult.Health.LSPStatus})

	return c, nil
}

// Events returns the channel of streaming events.
// Closed when the connection terminates (subprocess exit or Close).
func (c *Client) Events() <-chan Event {
	return c.events
}

// AgentRunOptions controls an agent.run or session.message call from the TUI.
type AgentRunOptions struct {
	Apply       bool
	AllowExec   bool
	Profile     string
	Attachments []RPCAttachment
}

// RPCAttachment mirrors core message attachment params.
type RPCAttachment struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
	MIME string `json:"mime,omitempty"`
	Name string `json:"name,omitempty"`
}

// SessionStart creates or reopens a core session.
func (c *Client) SessionStart(ctx context.Context, sessionID string) (string, bool, error) {
	params := map[string]any{}
	if strings.TrimSpace(sessionID) != "" {
		params["session_id"] = strings.TrimSpace(sessionID)
	}
	var res struct {
		SessionID string `json:"session_id"`
		Restored  bool   `json:"restored"`
	}
	if err := c.rpc.Call(ctx, "session.start", params, &res); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(res.SessionID) == "" {
		return "", false, fmt.Errorf("session.start returned empty session_id")
	}
	return res.SessionID, res.Restored, nil
}

// SessionGetResult mirrors core.SessionGetResult.
type SessionGetResult struct {
	SessionID  string                  `json:"session_id"`
	Title      string                  `json:"title,omitempty"`
	Model      string                  `json:"model,omitempty"`
	UIMessages []sessionfile.UIMessage `json:"ui_messages"`
	Todos      []TodoItem              `json:"todos,omitempty"`
	PlanPath   string                  `json:"plan_path,omitempty"`
	CostUSD    float64                 `json:"cost_usd,omitempty"`
	HistoryLen int                     `json:"history_len"`
	HasPending bool                    `json:"has_pending,omitempty"`
	Restored   bool                    `json:"restored,omitempty"`
	// ExternalTurn: a detached background core is still finishing a turn on
	// this session. Interrupted: the previous turn holder died mid-turn.
	ExternalTurn bool `json:"external_turn,omitempty"`
	ExternalPID  int  `json:"external_pid,omitempty"`
	Interrupted  bool `json:"interrupted,omitempty"`
}

// SessionGet returns the unified v2 session view.
func (c *Client) SessionGet(ctx context.Context, sessionID string) (*SessionGetResult, error) {
	var res SessionGetResult
	if err := c.rpc.Call(ctx, "session.get", map[string]any{
		"session_id": sessionID,
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SessionUISync persists the TUI chat projection (and optional session spend).
func (c *Client) SessionUISync(ctx context.Context, sessionID, title, model string, ui []sessionfile.UIMessage, costUSD float64) error {
	params := map[string]any{
		"session_id":  sessionID,
		"title":       title,
		"model":       model,
		"ui_messages": ui,
	}
	if costUSD > 0 {
		params["cost_usd"] = costUSD
	}
	var res map[string]any
	return c.rpc.Call(ctx, "session.ui_sync", params, &res)
}

// SessionRewindResult mirrors core.SessionRewindResult.
type SessionRewindResult struct {
	SessionID       string `json:"session_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

// SessionRewind truncates UI projection and LLM history to a user checkpoint.
func (c *Client) SessionRewind(ctx context.Context, sessionID string, uiMessageIndex int) (*SessionRewindResult, error) {
	var res SessionRewindResult
	err := c.rpc.Call(ctx, "session.rewind", map[string]any{
		"session_id":       sessionID,
		"ui_message_index": uiMessageIndex,
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// SessionMessage runs one agent turn in an existing session. Streaming events
// arrive via Events(). Replaces one-shot agent.run for multi-turn chat.
func (c *Client) SessionMessage(ctx context.Context, sessionID, query, mode string, opts AgentRunOptions) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	params := map[string]any{
		"session_id": sessionID,
		"content":    query,
		"apply":      opts.Apply,
		"backup":     opts.Apply,
		"allow_exec": opts.AllowExec,
	}
	if mode != "" {
		params["mode"] = mode
	}
	if opts.Profile != "" {
		params["profile"] = opts.Profile
	}
	if len(opts.Attachments) > 0 {
		params["attachments"] = opts.Attachments
	}
	var result struct {
		Usage            *UsageTurnPayload `json:"usage"`
		Todos            []TodoItem        `json:"todos"`
		StopReason       string            `json:"stop_reason"`
		MaxStepsExceeded bool              `json:"max_steps_exceeded"`
		OpenTodos        int               `json:"open_todos"`
	}
	err := c.rpc.Call(ctx, "session.message", params, &result)
	if err != nil {
		c.send(Event{Kind: EventError, Err: err.Error()})
	} else {
		if result.Usage != nil {
			c.send(Event{Kind: EventTurnUsage, Usage: result.Usage})
		}
		c.send(Event{
			Kind:       EventTurnTodos,
			Todos:      result.Todos,
			StopReason: result.StopReason,
			OpenTodos:  result.OpenTodos,
			Content:    result.StopReason,
		})
		if result.MaxStepsExceeded && result.StopReason == "" {
			// keep Content for older cores
		}
	}
	c.send(Event{Kind: EventAgentRunCompleted})
	return err
}

// AgentRun calls agent.run on the core (one-shot, no session history).
// Prefer SessionMessage for interactive TUI chat.
func (c *Client) AgentRun(ctx context.Context, query, mode string, opts AgentRunOptions) error {
	params := map[string]any{
		"query":      query,
		"apply":      opts.Apply,
		"backup":     opts.Apply,
		"allow_exec": opts.AllowExec,
	}
	if mode != "" {
		params["mode"] = mode
	}
	if len(opts.Attachments) > 0 {
		params["attachments"] = opts.Attachments
	}
	var result map[string]any
	err := c.rpc.Call(ctx, "agent.run", params, &result)
	if err != nil {
		c.send(Event{Kind: EventError, Err: err.Error()})
	}
	c.send(Event{Kind: EventAgentRunCompleted})
	return err
}

// WorkflowSummary is a lightweight view of a workflow returned by
// workflow.list. Mirrors core.WorkflowSummary.
type WorkflowSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Stages      []string `json:"stages"`
	Source      string   `json:"source,omitempty"`
}

// WorkflowList calls workflow.list on the core and returns the list.
func (c *Client) WorkflowList(ctx context.Context) ([]WorkflowSummary, error) {
	var res struct {
		Workflows []WorkflowSummary `json:"workflows"`
	}
	if err := c.rpc.Call(ctx, "workflow.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Workflows, nil
}

// WorkflowRunOptions controls a workflow.run call.
type WorkflowRunOptions struct {
	Apply        bool
	AllowExec    bool
	AllowWeb     bool
	AllowBrowser bool
}

// WorkflowRunResult mirrors core.WorkflowRunResult.
type WorkflowRunResult struct {
	Name          string            `json:"name"`
	Outputs       map[string]string `json:"outputs"`
	FinalStage    string            `json:"final_stage,omitempty"`
	FailureReason string            `json:"failure_reason,omitempty"`
	DurationMS    int64             `json:"duration_ms"`
}

// WorkflowRun invokes workflow.run on the core. Streaming stage events
// arrive via Events() (EventWorkflowStageStart / EventWorkflowStageDone).
func (c *Client) WorkflowRun(ctx context.Context, name, arguments string, opts WorkflowRunOptions) (*WorkflowRunResult, error) {
	params := map[string]any{
		"name":          name,
		"arguments":     arguments,
		"apply":         opts.Apply,
		"allow_exec":    opts.AllowExec,
		"allow_web":     opts.AllowWeb,
		"allow_browser": opts.AllowBrowser,
	}
	var res WorkflowRunResult
	if err := c.rpc.Call(ctx, "workflow.run", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SkillSummary mirrors core.SkillSummary.
type SkillSummary struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tools             []string `json:"tools,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model,omitempty"`
	CompletionMarkers []string `json:"completion_markers,omitempty"`
	Origin            string   `json:"origin,omitempty"`
}

// SkillList calls skill.list on the core and returns the list.
func (c *Client) SkillList(ctx context.Context) ([]SkillSummary, error) {
	var res struct {
		Skills []SkillSummary `json:"skills"`
	}
	if err := c.rpc.Call(ctx, "skill.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Skills, nil
}

// SkillInvokeOptions controls a skill.invoke call.
type SkillInvokeOptions struct {
	AllowExec    bool
	AllowWeb     bool
	AllowBrowser bool
}

// SkillInvokeResult mirrors core.SkillInvokeResult.
type SkillInvokeResult struct {
	Skill  string `json:"skill"`
	Output string `json:"output"`
	Marker string `json:"marker,omitempty"`
	Steps  int    `json:"steps"`
}

// SkillInvoke calls skill.invoke on the core.
func (c *Client) SkillInvoke(ctx context.Context, name, arguments string, opts SkillInvokeOptions) (*SkillInvokeResult, error) {
	params := map[string]any{
		"name":          name,
		"arguments":     arguments,
		"allow_exec":    opts.AllowExec,
		"allow_web":     opts.AllowWeb,
		"allow_browser": opts.AllowBrowser,
	}
	var res SkillInvokeResult
	if err := c.rpc.Call(ctx, "skill.invoke", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ApplyOps sends the given ops to the core for application (no LLM re-run).
func (c *Client) ApplyOps(ctx context.Context, rawOps []map[string]any) error {
	params := map[string]any{
		"ops":    rawOps,
		"backup": true,
	}
	var result map[string]any
	return c.rpc.Call(ctx, "ops.apply", params, &result)
}

// ToolCall invokes a core tool synchronously (e.g. fs.delete for diff revert).
func (c *Client) ToolCall(ctx context.Context, tool string, input json.RawMessage) error {
	params := map[string]any{
		"name":  tool,
		"input": json.RawMessage(input),
	}
	var result map[string]any
	return c.rpc.Call(ctx, "tool.call", params, &result)
}

// coreDetachGrace is how long Close waits for the core subprocess to exit
// after stdin EOF before detaching (leaving it to finish in the background).
var coreDetachGrace = 2 * time.Second

// Close closes the subprocess stdin (EOF makes the core's Serve loop return)
// and waits briefly for it to exit. If the core is still busy — e.g. an agent
// turn is in flight — it is deliberately NOT killed: the core finishes the
// turn, persists session snapshots and exits on its own (resilience audit P3).
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		// Unblock producers first: any goroutine blocked in a critical-event
		// send selects on done and bails out.
		close(c.done)
		// Wait for all in-flight sends to finish, then flip closed so no new
		// send can touch the channel after this point.
		c.sendMu.Lock()
		c.closed = true
		c.sendMu.Unlock()

		// Deny all outstanding permission/question requests so the core-side
		// turn unwinds instead of waiting forever.
		c.failPendingRequests()

		_ = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				_, _ = c.cmd.Process.Wait()
			}()
			select {
			case <-exited:
				// Fast path: idle core exits right after EOF.
			case <-time.After(coreDetachGrace):
				// Turn still running — detach. The core self-terminates once
				// the turn completes (Serve's dispatchWG drains, defers run).
			}
		}
		close(c.events)
	})
	return nil
}

// send delivers an event to the TUI. Coalescable stream deltas may be merged
// when the channel is saturated; every other event kind is delivered
// reliably (blocking until the consumer drains the channel or the client is
// closed) — losing e.g. EventAgentRunCompleted or EventPermissionRequest
// would wedge the UI or deadlock the agent turn.
func (c *Client) send(ev Event) {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.closed {
		return
	}
	c.flushCoalesce()
	if c.trySend(ev) {
		return
	}
	if c.mergeCoalesce(ev) {
		return
	}
	// Critical event with a full channel: deliver pending deltas first so
	// ordering is preserved, then block on the event itself.
	c.drainCoalesceBlocking()
	select {
	case c.events <- ev:
	case <-c.done:
	}
}

func (c *Client) trySend(ev Event) bool {
	select {
	case c.events <- ev:
		return true
	default:
		return false
	}
}

// isCoalescable reports whether consecutive events of this kind can be merged
// into one without losing information.
func isCoalescable(k EventKind) bool {
	switch k {
	case EventMessageDelta, EventReasoningDelta, EventToolCallDelta, EventExecOutputChunk:
		return true
	}
	return false
}

// flushCoalesce pushes as many queued deltas as fit into the channel (FIFO).
func (c *Client) flushCoalesce() {
	c.coalesceMu.Lock()
	defer c.coalesceMu.Unlock()
	for len(c.coalesce) > 0 {
		select {
		case c.events <- c.coalesce[0]:
			c.coalesce = c.coalesce[1:]
		default:
			return
		}
	}
}

// drainCoalesceBlocking delivers every queued delta, blocking on a full
// channel. Used right before a blocking critical-event send.
func (c *Client) drainCoalesceBlocking() {
	for {
		c.coalesceMu.Lock()
		if len(c.coalesce) == 0 {
			c.coalesceMu.Unlock()
			return
		}
		pending := c.coalesce[0]
		c.coalesce = c.coalesce[1:]
		c.coalesceMu.Unlock()
		select {
		case c.events <- pending:
		case <-c.done:
			return
		}
	}
}

// mergeCoalesce appends ev to the delta queue, merging with the queue tail
// when kinds (and tool-call ids) match. Returns false for non-coalescable
// kinds — the caller must deliver those reliably.
func (c *Client) mergeCoalesce(ev Event) bool {
	if !isCoalescable(ev.Kind) {
		return false
	}
	c.coalesceMu.Lock()
	defer c.coalesceMu.Unlock()
	if n := len(c.coalesce); n > 0 {
		last := &c.coalesce[n-1]
		if last.Kind == ev.Kind {
			switch ev.Kind {
			case EventMessageDelta, EventReasoningDelta, EventExecOutputChunk:
				last.Content += ev.Content
				return true
			case EventToolCallDelta:
				if last.ToolCallID == ev.ToolCallID {
					last.ArgsDelta += ev.ArgsDelta
					return true
				}
			}
		}
	}
	c.coalesce = append(c.coalesce, ev)
	return true
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "agent/event":
		c.handleAgentEvent(params)
	case "exec/output_chunk":
		c.handleExecOutput(params)
	case "workflow/stage_start":
		c.handleWorkflowStage(EventWorkflowStageStart, params)
	case "workflow/stage_done":
		c.handleWorkflowStage(EventWorkflowStageDone, params)
	}
}

func (c *Client) handleWorkflowStage(kind EventKind, params json.RawMessage) {
	var p WorkflowStagePayload
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	c.send(Event{Kind: kind, Stage: &p})
}

func (c *Client) handleAgentEvent(params json.RawMessage) {
	var p struct {
		Step         int             `json:"step"`
		Type         string          `json:"type"`
		SessionID    string          `json:"session_id"`
		TurnID       string          `json:"turn_id"`
		Content      string          `json:"content"`
		Error        string          `json:"error"`
		ToolCallID   string          `json:"tool_call_id"`
		ToolCallName string          `json:"tool_call_name"`
		ArgsDelta    string          `json:"args_delta"`
		Data         json.RawMessage `json:"data"`
		Diagnostics  json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	ev := Event{
		Kind:         EventKind(p.Type),
		Step:         p.Step,
		SessionID:    p.SessionID,
		TurnID:       p.TurnID,
		Content:      p.Content,
		ToolCallID:   p.ToolCallID,
		ToolCallName: p.ToolCallName,
		ArgsDelta:    p.ArgsDelta,
	}
	if EventKind(p.Type) == EventPendingOps && len(p.Data) > 0 {
		var payload PendingOpsPayload
		if err := json.Unmarshal(p.Data, &payload); err == nil {
			ev.PendingOps = &payload
		}
	}
	if EventKind(p.Type) == EventStepUsage && len(p.Data) > 0 {
		var usage UsageTurnPayload
		if err := json.Unmarshal(p.Data, &usage); err == nil {
			ev.Usage = &usage
		}
	}
	if EventKind(p.Type) == EventModeRoute && len(p.Data) > 0 {
		var route ModeRoutePayload
		if err := json.Unmarshal(p.Data, &route); err == nil {
			ev.ModeRoute = &route
		}
	}
	if EventKind(p.Type) == EventError {
		errMsg := strings.TrimSpace(p.Error)
		if errMsg == "" {
			errMsg = strings.TrimSpace(p.Content)
		}
		ev.Err = errMsg
	}
	if EventKind(p.Type) == EventTodosUpdated && strings.TrimSpace(p.Content) != "" {
		var items []TodoItem
		if err := json.Unmarshal([]byte(p.Content), &items); err == nil {
			ev.Todos = items
		}
	}
	if len(p.Diagnostics) > 0 && string(p.Diagnostics) != "null" {
		var diags []ToolDiagnosticPayload
		if err := json.Unmarshal(p.Diagnostics, &diags); err == nil {
			ev.Diagnostics = diags
		}
	}
	c.send(ev)
}

func (c *Client) handleExecOutput(params json.RawMessage) {
	var p struct {
		Step      int    `json:"step"`
		Chunk     string `json:"chunk"`
		SessionID string `json:"session_id"`
		TurnID    string `json:"turn_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	c.send(Event{
		Kind:      EventExecOutputChunk,
		Step:      p.Step,
		SessionID: p.SessionID,
		TurnID:    p.TurnID,
		Content:   p.Chunk,
	})
}

func (c *Client) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "permission/request":
		var req PermissionRequestPayload
		if err := json.Unmarshal(params, &req); err != nil {
			return map[string]any{"approved": false}, nil
		}
		id, ch := c.registerPermWaiter()
		defer c.unregisterPermWaiter(id)
		req.ReqID = id
		c.send(Event{Kind: EventPermissionRequest, ReqID: id, PermReq: &req})
		select {
		case d := <-ch:
			return map[string]any{"approved": d.Approved, "always": d.Always}, nil
		case <-ctx.Done():
			return map[string]any{"approved": false}, nil
		case <-c.done:
			return map[string]any{"approved": false}, nil
		}
	case "question/ask":
		var req struct {
			Questions []QuestionItemPayload `json:"questions"`
		}
		if err := json.Unmarshal(params, &req); err != nil || len(req.Questions) == 0 {
			return map[string]any{"answers": []string{}}, nil
		}
		id, ch := c.registerQuestionWaiter()
		defer c.unregisterQuestionWaiter(id)
		c.send(Event{Kind: EventQuestionAsked, ReqID: id, Questions: req.Questions})
		select {
		case answers := <-ch:
			return map[string]any{"answers": answers}, nil
		case <-ctx.Done():
			return map[string]any{"answers": []string{}}, nil
		case <-c.done:
			return map[string]any{"answers": []string{}}, nil
		}
	default:
		return nil, fmt.Errorf("unsupported server request: %s", method)
	}
}

func (c *Client) registerPermWaiter() (int64, chan PermissionDecision) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.reqSeq++
	id := c.reqSeq
	ch := make(chan PermissionDecision, 1)
	if c.permWaiters == nil {
		c.permWaiters = map[int64]chan PermissionDecision{}
	}
	c.permWaiters[id] = ch
	return id, ch
}

func (c *Client) unregisterPermWaiter(id int64) {
	c.reqMu.Lock()
	delete(c.permWaiters, id)
	c.reqMu.Unlock()
}

func (c *Client) registerQuestionWaiter() (int64, chan []string) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.reqSeq++
	id := c.reqSeq
	ch := make(chan []string, 1)
	if c.questionWaiters == nil {
		c.questionWaiters = map[int64]chan []string{}
	}
	c.questionWaiters[id] = ch
	return id, ch
}

func (c *Client) unregisterQuestionWaiter(id int64) {
	c.reqMu.Lock()
	delete(c.questionWaiters, id)
	c.reqMu.Unlock()
}

// failPendingRequests denies every outstanding permission/question request.
// Called from Close so core-side turns unwind instead of waiting forever.
func (c *Client) failPendingRequests() {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	for id, ch := range c.permWaiters {
		select {
		case ch <- PermissionDecision{Approved: false}:
		default:
		}
		delete(c.permWaiters, id)
	}
	for id, ch := range c.questionWaiters {
		select {
		case ch <- nil:
		default:
		}
		delete(c.questionWaiters, id)
	}
}

// RespondPermission answers the permission/request identified by reqID.
// A stale or duplicate answer (unknown id) is silently dropped — it can
// never be misdelivered to a different request.
func (c *Client) RespondPermission(reqID int64, approved bool) {
	c.RespondPermissionDecision(reqID, PermissionDecision{Approved: approved})
}

// RespondPermissionDecision answers with approved + always (lsp.auto_install).
func (c *Client) RespondPermissionDecision(reqID int64, d PermissionDecision) {
	c.reqMu.Lock()
	ch, ok := c.permWaiters[reqID]
	if ok {
		delete(c.permWaiters, reqID)
	}
	c.reqMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- d:
	default:
	}
}

// RespondQuestion answers the question/ask identified by reqID.
func (c *Client) RespondQuestion(reqID int64, answers []string) {
	c.reqMu.Lock()
	ch, ok := c.questionWaiters[reqID]
	if ok {
		delete(c.questionWaiters, reqID)
	}
	c.reqMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- answers:
	default:
	}
}

// QueryLSPStatus calls core.health and returns lsp_status (off|idle|installing|active).
func (c *Client) QueryLSPStatus(ctx context.Context) (string, error) {
	st, _, _, err := c.QueryLSPStatusDetail(ctx)
	return st, err
}

// QueryLSPStatusDetail returns lsp_status plus optional install progress %.
func (c *Client) QueryLSPStatusDetail(ctx context.Context) (status string, percent int, id string, err error) {
	if c == nil || c.rpc == nil {
		return "", 0, "", fmt.Errorf("rpcclient: not connected")
	}
	var h struct {
		LSPStatus          string `json:"lsp_status"`
		LSPInstallProgress *struct {
			ID      string `json:"id"`
			Percent int    `json:"percent"`
			Message string `json:"message"`
		} `json:"lsp_install_progress"`
	}
	if err := c.rpc.Call(ctx, "core.health", map[string]any{}, &h); err != nil {
		return "", 0, "", err
	}
	st := strings.TrimSpace(h.LSPStatus)
	if h.LSPInstallProgress != nil {
		return st, h.LSPInstallProgress.Percent, h.LSPInstallProgress.ID, nil
	}
	return st, 0, "", nil
}

// SessionCompactResult mirrors core.SessionCompactResult.
type SessionCompactResult struct {
	SessionID  string `json:"session_id"`
	BeforeMsgs int    `json:"before_msgs"`
	AfterMsgs  int    `json:"after_msgs"`
}

// SessionCompact forces LLM history compaction for the session.
func (c *Client) SessionCompact(ctx context.Context, sessionID, query string) (*SessionCompactResult, error) {
	if c == nil || c.rpc == nil {
		return nil, fmt.Errorf("rpcclient: not connected")
	}
	params := map[string]any{"session_id": strings.TrimSpace(sessionID)}
	if q := strings.TrimSpace(query); q != "" {
		params["query"] = q
	}
	var res SessionCompactResult
	if err := c.rpc.Call(ctx, "session.compact", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

package rpcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/protocol"
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

	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool

	coalesceMu sync.Mutex
	coalesce   Event // merged delta when events channel is saturated

	permCh     chan PermissionDecision // receives user's decision for permission/request
	questionCh chan []string           // receives user's answers for pending question/ask
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

	cmd := exec.CommandContext(ctx, cfg.Binary, "core", "--workspace-root", cfg.WorkspaceRoot)
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
		cfg:        cfg,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		rpc:        jsonrpc.NewClient(stdout, stdin),
		events:     make(chan Event, 64),
		permCh:     make(chan PermissionDecision, 1),
		questionCh: make(chan []string, 1),
	}

	// Drain stderr to avoid pipe blocking.
	go func() {
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
	Apply     bool
	AllowExec bool
	Profile   string
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

// Close kills the subprocess and closes the events channel.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()

		_ = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		close(c.events)
	})
	return nil
}

func (c *Client) send(ev Event) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	c.flushCoalesce()
	if c.trySend(ev) {
		return
	}
	if c.mergeCoalesce(ev) {
		return
	}
	// Non-coalescable or coalesce buffer full: one blocking attempt.
	select {
	case c.events <- ev:
	default:
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

func (c *Client) flushCoalesce() {
	c.coalesceMu.Lock()
	pending := c.coalesce
	c.coalesce = Event{}
	c.coalesceMu.Unlock()
	if pending.Kind == "" {
		return
	}
	select {
	case c.events <- pending:
	default:
		c.coalesceMu.Lock()
		if c.coalesce.Kind == "" {
			c.coalesce = pending
		} else {
			c.mergeCoalesceLocked(pending)
		}
		c.coalesceMu.Unlock()
	}
}

func (c *Client) mergeCoalesce(ev Event) bool {
	c.coalesceMu.Lock()
	defer c.coalesceMu.Unlock()
	return c.mergeCoalesceLocked(ev)
}

func (c *Client) mergeCoalesceLocked(ev Event) bool {
	switch ev.Kind {
	case EventMessageDelta:
		if c.coalesce.Kind == EventMessageDelta {
			c.coalesce.Content += ev.Content
			return true
		}
		c.coalesce = ev
		return true
	case EventToolCallDelta:
		if c.coalesce.Kind == EventToolCallDelta && c.coalesce.ToolCallID == ev.ToolCallID {
			c.coalesce.ArgsDelta += ev.ArgsDelta
			return true
		}
		c.coalesce = ev
		return true
	default:
		return false
	}
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
		c.send(Event{Kind: EventPermissionRequest, PermReq: &req})
		select {
		case d := <-c.permCh:
			return map[string]any{"approved": d.Approved, "always": d.Always}, nil
		case <-ctx.Done():
			return map[string]any{"approved": false}, nil
		}
	case "question/ask":
		var req struct {
			Questions []QuestionItemPayload `json:"questions"`
		}
		if err := json.Unmarshal(params, &req); err != nil || len(req.Questions) == 0 {
			return map[string]any{"answers": []string{}}, nil
		}
		c.send(Event{Kind: EventQuestionAsked, Questions: req.Questions})
		select {
		case answers := <-c.questionCh:
			return map[string]any{"answers": answers}, nil
		case <-ctx.Done():
			return map[string]any{"answers": []string{}}, nil
		}
	default:
		return nil, fmt.Errorf("unsupported server request: %s", method)
	}
}

// RespondPermission answers the pending permission/request from the core.
// Must be called exactly once per EventPermissionRequest event.
func (c *Client) RespondPermission(approved bool) {
	c.RespondPermissionDecision(PermissionDecision{Approved: approved})
}

// RespondPermissionDecision answers with approved + always (lsp.auto_install).
func (c *Client) RespondPermissionDecision(d PermissionDecision) {
	select {
	case c.permCh <- d:
	default:
	}
}

// RespondQuestion answers the pending question/ask from the core.
// Must be called exactly once per EventQuestionAsked event.
func (c *Client) RespondQuestion(answers []string) {
	select {
	case c.questionCh <- answers:
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

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/tools"
)
// ── Session API ──────────────────────────────────────────────────────────────

type SessionStartParams struct {
	// SessionID optionally reopens an existing on-disk session (v2 snapshot).
	// When empty, core allocates a new sortable id.
	SessionID string `json:"session_id,omitempty"`
}

type SessionStartResult struct {
	SessionID string `json:"session_id"`
	Restored  bool   `json:"restored,omitempty"`
}

// SessionStart creates or reopens a session and returns its canonical id.
func (c *Core) SessionStart(params SessionStartParams) (*SessionStartResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	id := strings.TrimSpace(params.SessionID)
	var s *coresession.Session
	var restored bool
	if id != "" {
		var err error
		s, err = c.sessions.LoadOrCreate(c.workspaceRoot, id)
		if err != nil {
			return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": id})
		}
		s.Lock()
		restored = sessionLooksRestoredLocked(s)
		s.Unlock()
	} else {
		s = c.sessions.Create()
	}
	return &SessionStartResult{SessionID: s.ID, Restored: restored}, nil
}

// sessionLooksRestoredLocked reports whether a loaded session carries any
// durable state worth a TUI SessionGet (history, UI, todos, or plan path).
// Caller must hold sess.Lock.
func sessionLooksRestoredLocked(s *coresession.Session) bool {
	if s == nil {
		return false
	}
	return len(s.History) > 0 || len(s.UIMessages()) > 0 || len(s.CopyTodos()) > 0 || s.PlanPath() != ""
}

// persistSessionTodos writes todowrite payload into the session snapshot.
// Best-effort: snapshot failures are logged and ignored (turn still continues).
func persistSessionTodos(workspaceRoot string, sess *coresession.Session, payload string) {
	if sess == nil || strings.TrimSpace(payload) == "" {
		return
	}
	var items []tools.TodoItem
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return
	}
	sess.Lock()
	sess.SetTodos(items)
	if snapErr := sess.Snapshot(workspaceRoot); snapErr != nil {
		fmt.Fprintf(os.Stderr, "core: session %s mid-turn todo snapshot failed: %v\n", sess.ID, snapErr)
	}
	sess.Unlock()
}

type SessionGetParams struct {
	SessionID string `json:"session_id"`
}

type SessionGetResult struct {
	SessionID  string                  `json:"session_id"`
	Title      string                  `json:"title,omitempty"`
	Model      string                  `json:"model,omitempty"`
	UIMessages []sessionfile.UIMessage `json:"ui_messages"`
	Todos      []tools.TodoItem        `json:"todos,omitempty"`
	PlanPath   string                  `json:"plan_path,omitempty"`
	CostUSD    float64                 `json:"cost_usd,omitempty"`
	HistoryLen int                     `json:"history_len"`
	HasPending bool                    `json:"has_pending,omitempty"`
	Restored   bool                    `json:"restored,omitempty"`
}

// SessionGet returns the unified v2 session view for TUI reopen.
func (c *Core) SessionGet(params SessionGetParams) (*SessionGetResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	defer sess.Unlock()
	ui := sess.UIMessages()
	return &SessionGetResult{
		SessionID:  sess.ID,
		Title:      sess.Title(),
		Model:      sess.Model(),
		UIMessages: ui,
		Todos:      sess.CopyTodos(),
		PlanPath:   sess.PlanPath(),
		CostUSD:    sess.CostUSD(),
		HistoryLen: len(sess.History),
		HasPending: sess.HasPending(),
		Restored:   sessionLooksRestoredLocked(sess),
	}, nil
}

type SessionListParams struct{}

type SessionListResult struct {
	Sessions []sessionfile.Meta `json:"sessions"`
}

// SessionList returns session picker metadata from on-disk v2 snapshots.
func (c *Core) SessionList(_ SessionListParams) (*SessionListResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	metas, err := sessionfile.ListMeta(c.workspaceRoot)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	if metas == nil {
		metas = []sessionfile.Meta{}
	}
	return &SessionListResult{Sessions: metas}, nil
}

type SessionUISyncParams struct {
	SessionID  string                  `json:"session_id"`
	Title      string                  `json:"title,omitempty"`
	Model      string                  `json:"model,omitempty"`
	UIMessages []sessionfile.UIMessage `json:"ui_messages"`
	CostUSD    float64                 `json:"cost_usd,omitempty"`
}

type SessionUISyncResult struct {
	SessionID string `json:"session_id"`
	Saved     bool   `json:"saved"`
}

// SessionUISync persists the TUI chat projection into the unified v2 snapshot.
func (c *Core) SessionUISync(params SessionUISyncParams) (*SessionUISyncResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	sess.SetTitle(params.Title)
	if strings.TrimSpace(params.Model) != "" {
		sess.SetModel(params.Model)
	}
	sess.SetUIMessages(params.UIMessages)
	if params.CostUSD > 0 {
		sess.SetCostUSD(params.CostUSD)
	}
	sess.LastActivity = time.Now()
	snapErr := sess.Snapshot(c.workspaceRoot)
	sess.Unlock()
	if snapErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, snapErr.Error(), map[string]any{"session_id": params.SessionID})
	}
	return &SessionUISyncResult{SessionID: params.SessionID, Saved: true}, nil
}

type SessionMessageParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`

	Apply     bool `json:"apply,omitempty"`
	Backup    bool `json:"backup,omitempty"`
	AllowExec bool `json:"allow_exec,omitempty"`

	MaxSteps          int `json:"max_steps,omitempty"`
	MaxInvalidRetries int `json:"max_invalid_retries,omitempty"`
	MaxPromptBytes    int `json:"max_prompt_bytes,omitempty"`

	// Mode selects the agent mode or custom agent name (from agents: in .orchestra.yml).
	Mode string `json:"mode,omitempty"`

	// ApplyOutput / PatchPath / Profile mirror agent.run (see AgentRunParams).
	ApplyOutput string `json:"apply_output,omitempty"`
	PatchPath   string `json:"patch_path,omitempty"`
	Profile     string `json:"profile,omitempty"`

	// OnEvent is set programmatically by the RPC handler for streaming notifications.
	OnEvent func(method string, params any) `json:"-"`

	// PermissionRequester, if non-nil, is consulted before exec.run/bash runs.
	// Set programmatically by the RPC handler.
	PermissionRequester PermissionRequester `json:"-"`

	// QuestionAsker enables question tool and plan_exit approval in sessions.
	QuestionAsker tools.QuestionAsker `json:"-"`

	// Attachments are optional files/images for multimodal turns.
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

type SessionMessageResult struct {
	Steps   int  `json:"steps"`
	Applied bool `json:"applied"`

	Patches       []patches.Patch           `json:"patches,omitempty"`
	Ops           []ops.AnyOp               `json:"ops,omitempty"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`

	SwitchToBuild bool             `json:"switch_to_build,omitempty"`
	Todos         []tools.TodoItem `json:"todos,omitempty"`
	PlanPath      string           `json:"plan_path,omitempty"`
	PatchPath     string           `json:"patch_path,omitempty"`

	// Usage summarises token consumption for this turn.
	Usage *UsageSnapshot `json:"usage,omitempty"`

	EffectiveMode string `json:"effective_mode,omitempty"`
	RoutedFrom    string `json:"routed_from,omitempty"`

	// StopReason: completed | partial | max_steps — for TUI status notices.
	StopReason       string `json:"stop_reason,omitempty"`
	MaxStepsExceeded bool   `json:"max_steps_exceeded,omitempty"`
	OpenTodos        int    `json:"open_todos,omitempty"`
}

// SessionMessage runs one agent turn in the named session, streaming events via OnEvent.
func (c *Core) SessionMessage(ctx context.Context, params SessionMessageParams) (*SessionMessageResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	if strings.TrimSpace(params.Content) == "" && len(params.Attachments) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "content is empty", nil)
	}
	if err := validateTurnInput(params.Content, params.Attachments); err != nil {
		return nil, err
	}

	imageParts, err := loadAttachmentImages(c.cfg, c.workspaceRoot, params.Attachments)
	if err != nil {
		return nil, err
	}
	agentQuery := resolveTurnQuery(params.Content, params.Attachments, c.cfg != nil && c.cfg.LLM.Multimodal)
	agentQuery = enrichQueryWithImageHints(agentQuery, params.Attachments)

	applyOutput, err := resolveApplyOutput(c.cfg, params.ApplyOutput, &params.Apply, &params.Backup)
	if err != nil {
		return nil, err
	}

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	// Prevent concurrent turns on the same session.
	sess.Lock()
	if sess.IsBusy() {
		sess.Unlock()
		return nil, protocol.NewError(protocol.ExecFailed, "session is busy", map[string]any{"session_id": params.SessionID})
	}
	// Snapshot history and todos for this turn (under lock).
	inHistory := sess.CopyHistory()
	inTodos := sess.CopyTodos()
	planPath := sessionPlanPathLocked(sess, params.Mode)
	sess.AppendUIMessage(buildUserUIMessage(params.Content, params.Attachments))
	// Create a cancellable context for this turn and store its cancel in the session.
	turnCtx, cancel := context.WithCancel(ctx)
	sess.SetCancel(cancel)
	sess.Unlock()

	// Ensure cancel and session state are cleaned up on exit.
	defer func() {
		sess.Lock()
		sess.ClearCancel()
		sess.Unlock()
		cancel()
	}()

	// Same staging contract as AgentRun: serialise shared Runner mutations
	// (SetDryRun, ClearStaged, staged overlay writes) for the whole turn.
	// Tools stage write/edit; params.Apply commits at end and unlocks bash.
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.tools.SetDryRun(true)
	c.tools.ClearStaged()
	c.tools.SetAllowExecDespiteDryRun(params.Apply)
	defer c.tools.SetAllowExecDespiteDryRun(false)

	launch, err := c.prepareAgentLaunch(agentLaunchSpec{
		Mode:                params.Mode,
		Profile:             params.Profile,
		PlanPath:            planPath,
		SessionID:           params.SessionID,
		Query:               agentQuery,
		Apply:               params.Apply,
		Backup:              params.Backup,
		AllowExec:           params.AllowExec,
		Debug:               c.debug,
		MaxSteps:            params.MaxSteps,
		MaxInvalidRetries:   params.MaxInvalidRetries,
		MaxPromptBytes:      params.MaxPromptBytes,
		InitialTodos:        inTodos,
		AutoSessionMemory:   c.cfg.Agent.ResolvedAutoSessionMemory(),
		UsageLabel:          "session.turn",
		OnEvent:             params.OnEvent,
		EventEnvelope:       EventEnvelope{SessionID: params.SessionID, TurnID: NewTurnID()},
		PermissionRequester: params.PermissionRequester,
		QuestionAsker:       params.QuestionAsker,
		Attachments:         params.Attachments,
		UserImages:          imageParts,
		Multimodal:          len(imageParts) > 0,
	})
	if err != nil {
		return nil, err
	}
	c.tools.SetMemoryContext(params.SessionID, c.cfg.Memory.Resolve())

	// Persist todos as soon as todowrite succeeds so a crash / cancel mid-turn
	// (or reopen before SessionMessage returns) does not lose the checklist.
	innerOnEvent := launch.Opts.OnEvent
	launch.Opts.OnEvent = func(ev agent.AgentEvent) {
		if ev.Stream.Kind == llm.StreamEventTodosUpdated {
			persistSessionTodos(c.workspaceRoot, sess, ev.Stream.Content)
		}
		if innerOnEvent != nil {
			innerOnEvent(ev)
		}
	}

	ag, err := agent.New(launch.Custom.llmClient, c.validator, c.tools, launch.Opts)
	if err != nil {
		return nil, err
	}

	outHistory, res, err := ag.Run(turnCtx, inHistory, agentQuery)
	if err == nil {
		outHistory, res, err = maybeContinueBuildAfterPlan(turnCtx, launch.Custom.llmClient, c.validator, c.tools, launch.Opts, outHistory, res)
	}
	finalizeAgentUsage(launch.Usage, c.workspaceRoot)
	profileName := launch.Profile

	// Always persist whatever history we have — including failed turns.
	// Previously err != nil skipped ReplaceHistory, so TUI reopen showed the
	// chat (ui_sync) while agent history was empty and the model re-explored.
	c.persistSessionTurn(sess, outHistory, res, launch.Opts.PlanPath, profileName, applyOutput, params.Apply)

	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "agent returned nil result", nil)
	}

	c.maybeAutoSummaryMemory(ctx, sess.ID, outHistory, res)

	out := &SessionMessageResult{
		Steps:            res.Steps,
		Applied:          res.Applied,
		Patches:          res.Patches,
		Ops:              res.Ops,
		ApplyResponse:    res.ApplyResponse,
		SwitchToBuild:    res.SwitchToBuild,
		Todos:            res.Todos,
		PlanPath:         launch.Opts.PlanPath,
		Usage:            usageSnapshotFrom(launch.Usage),
		EffectiveMode:    launch.EffectiveMode,
		StopReason:       res.StopReason,
		MaxStepsExceeded: res.MaxStepsExceeded,
		OpenTodos:        countOpenTodoItems(res.Todos),
	}
	if out.StopReason == "" {
		if res.MaxStepsExceeded {
			out.StopReason = "max_steps"
		} else if out.OpenTodos > 0 {
			out.StopReason = "partial"
		} else {
			out.StopReason = "completed"
		}
	}
	if strings.EqualFold(launch.RequestedMode, string(agent.ModeAgent)) && launch.EffectiveMode != "" {
		out.RoutedFrom = string(agent.ModeAgent)
	}
	if applyOutput == config.ApplyOutputPatch {
		path, werr := c.writeAgentPatch(params.PatchPath, res)
		if werr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, werr.Error(), nil)
		}
		out.PatchPath = path
		out.Applied = false
	}
	return out, nil
}

// persistSessionTurn writes agent history (+ todos/pending when present) to the
// in-memory session and snapshots to disk. Safe to call after failed turns —
// empty outHistory is a no-op for ReplaceHistory only when nil; we still want
// to keep prior history if the agent returned nothing.
func countOpenTodoItems(todos []tools.TodoItem) int {
	n := 0
	for _, t := range todos {
		switch t.Status {
		case tools.TodoPending, tools.TodoInProgress:
			n++
		}
	}
	return n
}

func (c *Core) persistSessionTurn(
	sess *coresession.Session,
	outHistory []llm.Message,
	res *agent.Result,
	planPath, profileName, applyOutput string,
	apply bool,
) {
	if c == nil || sess == nil {
		return
	}
	sess.Lock()
	defer sess.Unlock()
	if outHistory != nil {
		// Full ReplaceHistory (not append-only): compaction may rewrite the prefix.
		sess.ReplaceHistory(outHistory)
	}
	if res != nil {
		sess.SetTodos(res.Todos)
		if !apply && len(res.Ops) > 0 {
			sess.SetPending(res.Ops)
		} else {
			sess.SetPending(nil)
		}
	}
	if planPath != "" {
		sess.SetPlanPath(planPath)
	}
	if profileName != "" {
		sess.SetProfile(profileName)
	}
	if applyOutput != "" {
		sess.SetApplyOutput(applyOutput)
	}
	if snapErr := sess.Snapshot(c.workspaceRoot); snapErr != nil {
		fmt.Fprintf(os.Stderr, "core: session %s snapshot failed: %v\n", sess.ID, snapErr)
	}
}

type SessionApplyPendingParams struct {
	SessionID string `json:"session_id"`
	Backup    bool   `json:"backup,omitempty"`
}

type SessionApplyPendingResult struct {
	Applied       bool                      `json:"applied"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`
}

// SessionApplyPending applies ops stored from the last dry-run turn of the session.
func (c *Core) SessionApplyPending(ctx context.Context, params SessionApplyPendingParams) (*SessionApplyPendingResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	sess.Lock()
	pendingOps := sess.TakePending()
	sess.Unlock()

	if len(pendingOps) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()
	resp, err := c.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    pendingOps,
		Backup: params.Backup,
	})
	if err != nil {
		// Restore pending so the user can retry. Prepend original ops to any
		// newer ops that a concurrent turn may have added while we were applying.
		sess.Lock()
		newer := sess.TakePending()
		sess.SetPending(append(pendingOps, newer...))
		sess.Unlock()
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &SessionApplyPendingResult{Applied: true, ApplyResponse: resp}, nil
}

type SessionHistoryParams struct {
	SessionID string `json:"session_id"`
}

type SessionHistoryResult struct {
	SessionID string        `json:"session_id"`
	Messages  []llm.Message `json:"messages"`
}

// SessionHistory returns the accumulated history for a session.
func (c *Core) SessionHistory(params SessionHistoryParams) (*SessionHistoryResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	msgs := sess.CopyHistory()
	sess.Unlock()
	return &SessionHistoryResult{SessionID: params.SessionID, Messages: msgs}, nil
}

type SessionCompactParams struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query,omitempty"` // optional goal hint for the summary
}

type SessionCompactResult struct {
	SessionID   string `json:"session_id"`
	BeforeMsgs  int    `json:"before_msgs"`
	AfterMsgs   int    `json:"after_msgs"`
	BeforeBytes int    `json:"before_bytes,omitempty"`
	AfterBytes  int    `json:"after_bytes,omitempty"`
}

// SessionCompact forces ModeCompaction on the session LLM history and persists
// the sticky checkpoint (UIMessages untouched).
func (c *Core) SessionCompact(ctx context.Context, params SessionCompactParams) (*SessionCompactResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Lock()
	if sess.IsBusy() {
		sess.Unlock()
		return nil, protocol.NewError(protocol.ExecFailed, "session is busy", map[string]any{"session_id": params.SessionID})
	}
	hist := sess.CopyHistory()
	sess.Unlock()
	if len(hist) == 0 {
		return &SessionCompactResult{SessionID: params.SessionID}, nil
	}

	launch, err := c.prepareAgentLaunch(agentLaunchSpec{
		Mode:   string(agent.ModeBuild),
		Apply:  false,
		Backup: true,
		Debug:  c.debug,
	})
	if err != nil {
		return nil, err
	}
	ag, err := agent.New(launch.Custom.llmClient, c.validator, c.tools, launch.Opts)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	goal := strings.TrimSpace(params.Query)
	if goal == "" {
		goal = "Summarize the session so far"
	}
	before := len(hist)
	compacted, cerr := ag.CompactNow(ctx, goal, hist)
	if cerr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, cerr.Error(), nil)
	}
	sess.Lock()
	sess.ReplaceHistory(compacted)
	_ = sess.Snapshot(c.workspaceRoot)
	sess.Unlock()
	return &SessionCompactResult{
		SessionID:  params.SessionID,
		BeforeMsgs: before,
		AfterMsgs:  len(compacted),
	}, nil
}

type SessionCancelParams struct {
	SessionID string `json:"session_id"`
}

// SessionCancel cancels the currently running turn in a session (no-op if idle).
func (c *Core) SessionCancel(params SessionCancelParams) error {
	if c == nil {
		return protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sess, err := c.sessions.Get(params.SessionID)
	if err != nil {
		return protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}
	sess.Cancel()
	return nil
}

type SessionCloseParams struct {
	SessionID string `json:"session_id"`
}

// SessionClose cancels any running turn and removes the session.
func (c *Core) SessionClose(params SessionCloseParams) error {
	if c == nil {
		return protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if sess, err := c.sessions.Get(params.SessionID); err == nil {
		sess.Cancel()
		c.sessions.Delete(params.SessionID)
	}
	// Remove any on-disk snapshot too — close is idempotent across
	// memory and disk, otherwise "closed" sessions would resurrect on
	// the next core restart.
	if err := coresession.DeleteSnapshot(c.workspaceRoot, params.SessionID); err != nil {
		fmt.Fprintf(os.Stderr, "core: session %s snapshot delete failed: %v\n", params.SessionID, err)
	}
	return nil
}
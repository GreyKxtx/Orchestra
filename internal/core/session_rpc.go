package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/cache"
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
	c.fireLifecycleHook(context.Background(), hooks.EventSessionStart, s.ID, map[string]any{
		"restored":       restored,
		"workspace_root": c.workspaceRoot,
	})
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
	// ExternalTurn is true while another core process (detached background
	// core) is still running a turn on this session. UIs should show a
	// neutral notice and poll session.get until it clears — the refreshed
	// history is picked up automatically.
	ExternalTurn bool `json:"external_turn,omitempty"`
	ExternalPID  int  `json:"external_pid,omitempty"`
	// Interrupted is true when a previous turn holder died mid-turn (stale
	// lock reclaimed). History is preserved up to the last persisted step.
	Interrupted bool `json:"interrupted,omitempty"`
}

// SessionGet returns the unified v2 session view for TUI reopen.
func (c *Core) SessionGet(params SessionGetParams) (*SessionGetResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	// Pick up snapshots written by a detached background core since we
	// last loaded this session.
	c.sessions.RefreshFromDiskIfNewer(c.workspaceRoot, params.SessionID)

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	// Turn-lock state: is another process still finishing a turn here?
	externalTurn := false
	externalPID := 0
	interrupted := false
	if info, stale := coresession.CheckTurnLock(c.workspaceRoot, params.SessionID); info != nil {
		switch {
		case stale:
			// Holder died mid-turn. History is intact up to the last
			// persisted step (mid-turn snapshots); reclaim the lock.
			coresession.ClearStaleTurnLock(c.workspaceRoot, params.SessionID)
			interrupted = true
		case info.PID != os.Getpid():
			externalTurn = true
			externalPID = info.PID
		}
	}

	sess.Lock()
	defer sess.Unlock()
	ui := sess.UIMessages()
	return &SessionGetResult{
		SessionID:    sess.ID,
		Title:        sess.Title(),
		Model:        sess.Model(),
		UIMessages:   ui,
		Todos:        sess.CopyTodos(),
		PlanPath:     sess.PlanPath(),
		CostUSD:      sess.CostUSD(),
		HistoryLen:   len(sess.History),
		HasPending:   sess.HasPending(),
		Restored:     sessionLooksRestoredLocked(sess),
		ExternalTurn: externalTurn,
		ExternalPID:  externalPID,
		Interrupted:  interrupted,
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
	// SessionCostUSD is the accumulated spend for the whole session so far.
	SessionCostUSD float64 `json:"session_cost_usd,omitempty"`

	EffectiveMode string `json:"effective_mode,omitempty"`
	RoutedFrom    string `json:"routed_from,omitempty"`

	// StopReason: completed | partial | max_steps — for TUI status notices.
	StopReason       string `json:"stop_reason,omitempty"`
	MaxStepsExceeded bool   `json:"max_steps_exceeded,omitempty"`
	OpenTodos        int    `json:"open_todos,omitempty"`

	// Memory reports what the end-of-turn memory writer did, so a client can
	// show it instead of the operator having to open .orchestra/memory by hand.
	// Nil when auto_summary_memory is off.
	Memory *MemoryNoteStatus `json:"memory,omitempty"`

	// RuleSuggestion is set when this turn's anti-pattern repeated on the
	// same file often enough to offer a rule for ORCHESTRA.md. The client
	// answers via lesson.rule_respond. Nil on every other turn.
	RuleSuggestion *RuleSuggestionPayload `json:"rule_suggestion,omitempty"`
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

	// Before anything is spent: a user_prompt_submit hook may refuse the turn
	// or add context the model cannot know.
	agentQuery, err = c.applyUserPromptHooks(ctx, params.SessionID, agentQuery)
	if err != nil {
		return nil, err
	}

	applyOutput, err := resolveApplyOutput(c.cfg, params.ApplyOutput, &params.Apply, &params.Backup)
	if err != nil {
		return nil, err
	}

	// A detached background core may have finished a turn and written a newer
	// snapshot since we loaded this session — pick it up before starting.
	c.sessions.RefreshFromDiskIfNewer(c.workspaceRoot, params.SessionID)

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, params.SessionID)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": params.SessionID})
	}

	// Cross-process turn lock: refuse to run a second turn on the same
	// session while a detached core is still finishing one — two parallel
	// agents would burn tokens twice and race on the snapshot file.
	releaseTurnLock, lockErr := coresession.AcquireTurnLock(c.workspaceRoot, params.SessionID)
	if lockErr != nil {
		var active *coresession.TurnActiveError
		if errors.As(lockErr, &active) {
			msg := "session is busy"
			if active.PID != os.Getpid() {
				msg = fmt.Sprintf("предыдущий ход этой сессии ещё завершается в фоновом процессе (pid %d) — подождите несколько секунд и повторите", active.PID)
			}
			return nil, protocol.NewError(protocol.ExecFailed, msg, map[string]any{
				"session_id": params.SessionID,
				"holder_pid": active.PID,
			})
		}
		// Lock is advisory: a filesystem error must not block the turn.
		fmt.Fprintf(os.Stderr, "core: session %s turn lock: %v\n", params.SessionID, lockErr)
		releaseTurnLock = func() {}
	}
	defer releaseTurnLock()

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
	// Record where this user turn's agent output will begin. This is the only
	// link between the UI projection and the LLM history: the agent builds a
	// fresh system+user+history slice per request (agent_step.go), so the
	// prompt just appended to the UI is never appended to History, and the
	// agent does inject synthetic role=user messages mid-run. Recorded at
	// turn *start* rather than turn end because OnStepHistory below replaces
	// History with partial-turn content — a turn-end computation would be
	// wrong for every turn during which a mid-turn snapshot fired.
	sess.AppendTurnStart(len(inHistory))
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
		if !params.Apply && ev.Stream.Kind == llm.StreamEventPendingOps {
			persistSessionPendingFromEvent(c.workspaceRoot, sess, ev.Stream.Content)
		}
		if innerOnEvent != nil {
			innerOnEvent(ev)
		}
	}

	// Mid-turn history persistence (resilience audit P2): snapshot the
	// accumulated agent history after tool steps (throttled) so a crash or
	// kill mid-turn loses at most a few seconds of LLM work instead of the
	// whole turn. persistSessionTurn below remains authoritative at turn end.
	// OnStepHistory is invoked synchronously from the single agent-loop
	// goroutine, so lastStepPersist needs no lock.
	const stepPersistInterval = 5 * time.Second
	var lastStepPersist time.Time
	launch.Opts.OnStepHistory = func(_ int, hist []llm.Message) {
		if time.Since(lastStepPersist) < stepPersistInterval {
			return
		}
		lastStepPersist = time.Now()
		persistMidTurnHistory(c.workspaceRoot, sess, hist)
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

	// Accumulate this turn's spend into the session before the snapshot so
	// clients (VS Code, TUI reopen) see the running total. TUI's ui_sync may
	// later overwrite with its own accumulated value — both track the same
	// per-turn usage, so last-writer-wins is fine.
	if _, _, _, _, turnCost := launch.Usage.Total(); turnCost > 0 {
		sess.Lock()
		sess.SetCostUSD(sess.CostUSD() + turnCost)
		sess.Unlock()
	}

	// Always persist whatever history we have — including failed turns.
	// Previously err != nil skipped ReplaceHistory, so TUI reopen showed the
	// chat (ui_sync) while agent history was empty and the model re-explored.
	c.persistSessionTurn(sess, outHistory, res, launch.Opts.PlanPath, profileName, applyOutput, params.Apply)

	// turn_end fires for failed turns too: an audit or notification hook that
	// only ever hears about successes reports a quiet day when the turn died.
	c.fireLifecycleHook(ctx, hooks.EventTurnEnd, params.SessionID, turnEndPayload(res, err))

	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "agent returned nil result", nil)
	}

	memoryStatus := c.maybeAutoSummaryMemory(ctx, sess.ID, outHistory, res)

	out := &SessionMessageResult{
		Memory:           memoryStatus,
		Steps:            res.Steps,
		Applied:          res.Applied,
		Patches:          res.Patches,
		Ops:              res.Ops,
		ApplyResponse:    res.ApplyResponse,
		SwitchToBuild:    res.SwitchToBuild,
		Todos:            res.Todos,
		PlanPath:         launch.Opts.PlanPath,
		Usage:            usageSnapshotFrom(launch.Usage),
		SessionCostUSD:   sess.CostUSD(),
		EffectiveMode:    launch.EffectiveMode,
		StopReason:       res.StopReason,
		MaxStepsExceeded: res.MaxStepsExceeded,
		OpenTodos:        countOpenTodoItems(res.Todos),
		RuleSuggestion:   ruleSuggestionPayload(res.RuleSuggestion),
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
func persistSessionPendingFromEvent(workspaceRoot string, sess *coresession.Session, content string) {
	if sess == nil || strings.TrimSpace(content) == "" {
		return
	}
	var payload struct {
		Ops     []ops.AnyOp `json:"ops"`
		Applied bool        `json:"applied"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return
	}
	if payload.Applied || len(payload.Ops) == 0 {
		return
	}
	sess.Lock()
	sess.SetPending(payload.Ops)
	sess.Unlock()
	if snapErr := sess.Snapshot(workspaceRoot); snapErr != nil {
		fmt.Fprintf(os.Stderr, "core: session %s pending snapshot failed: %v\n", sess.ID, snapErr)
	}
}

// persistMidTurnHistory writes partial-turn history to disk so a crash mid-turn
// loses at most a few seconds of LLM work.
//
// It deliberately does NOT touch the session's turn boundaries. The history it
// writes is a turn in progress, whose boundary SessionMessage already recorded
// when the turn started; appending here would invent a turn per tick, and
// recomputing here would place the current turn's boundary at the partial
// content's end.
func persistMidTurnHistory(workspaceRoot string, sess *coresession.Session, hist []llm.Message) {
	if sess == nil {
		return
	}
	sess.Lock()
	sess.ReplaceHistory(hist)
	snapErr := sess.Snapshot(workspaceRoot)
	sess.Unlock()
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "core: session %s mid-turn history snapshot failed: %v\n", sess.ID, snapErr)
	}
}

// applyCompactedHistory installs a compacted history and marks every recorded
// turn boundary unknown.
//
// Compaction rewrites history wholesale — internal/agent/compact.go replaces
// the array with a summary plus a verbatim tail — so every index recorded
// before it points into an array that no longer exists. Keeping them would
// make fork and rewind cut at a position that means nothing; marking them
// makes fork refuse honestly and rewind fall back to its pre-boundary
// behaviour.
//
// Marked rather than cleared, because TurnStarts is positional: entry k
// describes user turn k+1 and is reached by counting user messages in the UI
// projection, which /compact does not touch. Clearing left the array
// permanently shorter than that count, so every turn AFTER the compaction
// failed to resolve too and the session lost forking for good. The slots stay,
// the turns the compaction ran under refuse by name, and the next turn appends
// a real boundary that resolves. See sessionfile.TurnStartUnknown.
func applyCompactedHistory(sess *coresession.Session, compacted []llm.Message) {
	if sess == nil {
		return
	}
	sess.ReplaceHistory(compacted)
	sess.SetTurnStarts(sessionfile.MarkTurnStartsUnknown(sess.TurnStarts()))
}

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
		// ...and when the run says it did, the turn boundaries recorded
		// against the OLD array no longer describe this one. They are
		// bounds-checked downstream, so a stale index still cuts — which is
		// worse than not cutting at all: fork hands back a silently wrong
		// branch, and rewind (destructive and persisted) discards history a
		// correct cut would have kept. Marking them in the same locked
		// section that installs the rewritten history makes fork refuse
		// honestly and rewind fall back to its pre-boundary behaviour.
		// SessionCompact marks them the same way for the compaction it
		// drives itself (applyCompactedHistory); this covers the compaction
		// and truncation the agent performs mid-turn, which never reach it.
		//
		// EVERY existing entry is marked, not just this turn's. The agent's
		// mid-turn rewrite is not local to the current turn: compactHistory
		// replaces the array with a summary plus a tail, and the truncation
		// fallback drops entries off the FRONT (agent_run.go: the compaction
		// branch, both convergence-failure branches, and recoverFromOverflow).
		// Marking only the current turn would leave the earlier entries
		// in-range against the shortened array and therefore still cutting —
		// e.g. turns at 0/3/6 in a 9-message history, compacted to 4 messages:
		// entry 6 falls out of range and refuses, but entry 3 still "resolves"
		// into the summary, which is exactly the silently wrong branch this
		// rule exists to prevent.
		//
		// res == nil means we have NO information: every error return in the
		// agent loop yields (history, nil, err) — cancellation, an unreachable
		// LLM, a circuit breaker, a panic — and HistoryRewritten is stamped by
		// a defer that only fires on a non-nil result. The core persists a
		// failed turn's history deliberately (see the comment at the call
		// site), so a turn that compacted at step N and then died at step N+1
		// hands us a rewritten array with no way to learn that it was
		// rewritten. Marking is the conservative answer, and unlike the
		// clearing it replaces it costs only the turns already recorded: the
		// array keeps one slot per user turn, so the next turn's boundary
		// lands in the slot fork looks in and the session is forkable again
		// from there. An interrupted turn no longer ends forking for good.
		if res == nil || res.HistoryRewritten {
			sess.SetTurnStarts(sessionfile.MarkTurnStartsUnknown(sess.TurnStarts()))
		}
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
	SessionID string   `json:"session_id"`
	Backup    bool     `json:"backup,omitempty"`
	Paths     []string `json:"paths,omitempty"` // optional: apply only ops whose path matches one of these (workspace-relative)
}

type SessionApplyPendingResult struct {
	Applied       bool                      `json:"applied"`
	ApplyResponse *tools.FSApplyOpsResponse `json:"apply_response,omitempty"`
	RemainingOps  []ops.AnyOp               `json:"remaining_ops,omitempty"`
}

type SessionDiscardPendingParams struct {
	SessionID string `json:"session_id"`
}

type SessionDiscardPendingResult struct {
	Discarded bool `json:"discarded"`
}

// SessionDiscardPending drops staged overlay changes and session pending ops
// without writing to disk (VS Code / TUI "reject all").
func (c *Core) SessionDiscardPending(params SessionDiscardPendingParams) (*SessionDiscardPendingResult, error) {
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

	c.runMu.Lock()
	defer c.runMu.Unlock()
	hadStaging := c.tools != nil && c.tools.HasStagedChanges()
	sess.Lock()
	hadPending := len(sess.CopyPending()) > 0
	sess.SetPending(nil)
	sess.Unlock()
	if c.tools != nil {
		c.tools.ClearStaged()
	}
	if snapErr := sess.Snapshot(c.workspaceRoot); snapErr != nil {
		fmt.Fprintf(os.Stderr, "core: session %s discard snapshot failed: %v\n", sess.ID, snapErr)
	}
	return &SessionDiscardPendingResult{Discarded: hadPending || hadStaging}, nil
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
	allPending := sess.CopyPending()
	sess.Unlock()

	c.runMu.Lock()
	defer c.runMu.Unlock()

	// Live staging overlay is authoritative during an in-flight dry-run turn.
	if c.tools != nil && c.tools.HasStagedChanges() {
		return c.sessionApplyFromStaging(ctx, sess, params, allPending)
	}

	pendingOps, remaining := filterPendingOpsByPaths(allPending, params.Paths)
	if len(pendingOps) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}
	refreshPendingWriteHashes(c.workspaceRoot, pendingOps)

	resp, err := c.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    pendingOps,
		Backup: params.Backup,
	})
	if err != nil {
		// Restore pending so the user can retry. Prepend original ops to any
		// newer ops that a concurrent turn may have added while we were applying.
		sess.Lock()
		newer := sess.TakePending()
		sess.SetPending(append(allPending, newer...))
		sess.Unlock()
		return nil, rpcApplyError(err)
	}

	sess.Lock()
	if len(params.Paths) > 0 {
		sess.SetPending(remaining)
	} else {
		sess.SetPending(nil)
	}
	sess.Unlock()

	if len(remaining) == 0 {
		c.tools.ClearStaged()
	} else {
		for _, op := range pendingOps {
			if p := strings.TrimSpace(op.Path); p != "" {
				c.tools.UnstagePath(p)
			}
		}
	}

	return &SessionApplyPendingResult{
		Applied:       true,
		ApplyResponse: resp,
		RemainingOps:  remaining,
	}, nil
}

func (c *Core) sessionApplyFromStaging(
	ctx context.Context,
	sess *coresession.Session,
	params SessionApplyPendingParams,
	allPending []ops.AnyOp,
) (*SessionApplyPendingResult, error) {
	paths := c.tools.ListStagedPaths()
	if len(params.Paths) > 0 {
		paths = filterStagedPaths(paths, params.Paths)
	}
	if len(paths) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}

	merged := &tools.FSApplyOpsResponse{
		Diffs:        make([]applier.FileDiff, 0, len(paths)),
		ChangedFiles: make([]string, 0, len(paths)),
	}
	for _, path := range paths {
		resp, err := c.tools.CommitStagedPath(ctx, path, params.Backup)
		if err != nil {
			return nil, rpcApplyError(err)
		}
		if resp == nil {
			continue
		}
		merged.Diffs = append(merged.Diffs, resp.Diffs...)
		merged.ChangedFiles = append(merged.ChangedFiles, resp.ChangedFiles...)
	}
	if len(merged.ChangedFiles) == 0 && len(merged.Diffs) == 0 {
		return &SessionApplyPendingResult{Applied: false}, nil
	}

	var remaining []ops.AnyOp
	sess.Lock()
	if len(params.Paths) > 0 {
		_, remaining = filterPendingOpsByPaths(allPending, params.Paths)
		sess.SetPending(remaining)
	} else {
		sess.SetPending(nil)
	}
	sess.Unlock()

	return &SessionApplyPendingResult{
		Applied:       true,
		ApplyResponse: merged,
		RemainingOps:  remaining,
	}, nil
}

func rpcApplyError(err error) error {
	if pe, ok := protocol.AsError(err); ok {
		return pe
	}
	return protocol.NewError(protocol.ExecFailed, err.Error(), nil)
}

// refreshPendingWriteHashes re-reads disk so stored pending ops match the current
// file before apply (session reload paths where overlay was cleared).
func refreshPendingWriteHashes(workspaceRoot string, pendingOps []ops.AnyOp) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return
	}
	for i := range pendingOps {
		wa := pendingOps[i].WriteAtomic
		if wa == nil {
			continue
		}
		rel := filepath.ToSlash(strings.TrimSpace(wa.Path))
		if rel == "" {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		b, err := os.ReadFile(abs)
		if err != nil {
			if os.IsNotExist(err) {
				wa.Conditions.FileHash = ""
				wa.Conditions.MustNotExist = true
			}
			continue
		}
		wa.Conditions.MustNotExist = false
		wa.Conditions.FileHash = cache.ComputeSHA256(b)
	}
}

func filterStagedPaths(staged []string, paths []string) []string {
	if len(staged) == 0 || len(paths) == 0 {
		return staged
	}
	out := make([]string, 0, len(staged))
	for _, p := range staged {
		if pendingPathMatches(p, paths) {
			out = append(out, p)
		}
	}
	return out
}

func filterPendingOpsByPaths(all []ops.AnyOp, paths []string) (toApply, remaining []ops.AnyOp) {
	if len(all) == 0 {
		return nil, nil
	}
	if len(paths) == 0 {
		return append([]ops.AnyOp(nil), all...), nil
	}
	toApply = make([]ops.AnyOp, 0, len(all))
	remaining = make([]ops.AnyOp, 0)
	for _, op := range all {
		if pendingPathMatches(op.Path, paths) {
			toApply = append(toApply, op)
		} else {
			remaining = append(remaining, op)
		}
	}
	return toApply, remaining
}

func pendingPathMatches(opPath string, paths []string) bool {
	opPath = filepath.ToSlash(strings.TrimSpace(opPath))
	if opPath == "" {
		return false
	}
	for _, raw := range paths {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		if pendingPathsEqual(opPath, p) {
			return true
		}
	}
	return false
}

func pendingPathsEqual(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	a = filepath.ToSlash(a)
	b = filepath.ToSlash(b)
	if runtime.GOOS == "windows" {
		if strings.EqualFold(a, b) {
			return true
		}
	} else if a == b {
		return true
	}
	if strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a) {
		return true
	}
	baseA := filepath.Base(a)
	baseB := filepath.Base(b)
	return baseA != "" && baseA == baseB
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
	c.sessions.RefreshFromDiskIfNewer(c.workspaceRoot, params.SessionID)
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
	applyCompactedHistory(sess, compacted)
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
	// Session memory (turn digests, session notes) is useless without the
	// session — deleting it here keeps .orchestra/memory/sessions/ from
	// accumulating orphan files. filepath.Base guards against path escape
	// via a crafted session_id.
	if sid := filepath.Base(strings.TrimSpace(params.SessionID)); sid != "" && sid != "." && sid != string(filepath.Separator) {
		memDir := filepath.Join(c.workspaceRoot, ".orchestra", "memory", "sessions")
		_ = os.Remove(filepath.Join(memDir, sid+".md"))
		_ = os.Remove(filepath.Join(memDir, sid+".turns.md"))
	}
	return nil
}
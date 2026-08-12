package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func rpcEventErrorText(ev rpcclient.Event) string {
	if msg := strings.TrimSpace(ev.Err); msg != "" {
		return msg
	}
	return strings.TrimSpace(ev.Content)
}

func isSilentAgentRetry(msg string) bool {
	return strings.Contains(msg, "Model returned an empty response") ||
		strings.Contains(msg, "Пустой ответ модели")
}

func isContextCompactedNotice(msg string) bool {
	return strings.HasPrefix(strings.TrimSpace(msg), "CONTEXT_COMPACTED")
}

func isContextPressureNotice(msg string) bool {
	return strings.HasPrefix(strings.TrimSpace(msg), "CONTEXT_PRESSURE")
}

func isMaxStepsNotice(msg string) bool {
	m := strings.TrimSpace(msg)
	return strings.HasPrefix(m, "MAX_STEPS") || strings.Contains(m, "max_steps exceeded")
}

func (a *App) noticeTurnStop(stopReason string, openTodos int, todos []rpcclient.TodoItem) {
	if openTodos <= 0 && len(todos) > 0 {
		for _, t := range todos {
			switch strings.ToLower(strings.TrimSpace(t.Status)) {
			case "pending", "in_progress":
				openTodos++
			}
		}
	}
	reason := strings.ToLower(strings.TrimSpace(stopReason))
	var msg string
	var kind state.SystemKind
	switch reason {
	case "max_steps":
		msg = "Лимит шагов — ход прерван (не ошибка сети). Напиши «продолжай»."
		if openTodos > 0 {
			msg = fmt.Sprintf("Лимит шагов — ход прерван. Ещё %d задач открыто. Напиши «продолжай».", openTodos)
		}
		kind = state.SystemKindInfo
	case "partial":
		msg = fmt.Sprintf("Ход завершён частично: ещё %d задач в списке. Это не обрыв связи — напиши «продолжай».", openTodos)
		kind = state.SystemKindInfo
	case "completed":
		// Normal success — the assistant reply is enough; skip footer noise.
		return
	default:
		if openTodos > 0 {
			msg = fmt.Sprintf("Ход завершён частично: ещё %d задач в списке. Напиши «продолжай».", openTodos)
			kind = state.SystemKindInfo
		} else {
			return
		}
	}
	if msg != "" {
		a.session.AppendAssistantNotice(kind, msg)
		a.chatDirty = true
	}
}

func stepDoneUserHint(reason string) string {
	switch reason {
	case "invalid":
		return "" // detailed hint comes via EventRecoverableError
	case "final_retry":
		return "ошибка apply/resolver — повтор final"
	default:
		return ""
	}
}

// listenForEvents returns a Cmd that reads one or more events from the rpc channel.
// The command is stamped with the current client generation: Update() drops
// messages (and does not re-arm the listener) when the generation no longer
// matches, so a respawn can never leave two competing consumers on one channel.
func (a *App) listenForEvents() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	ch := a.rpc.Events()
	gen := a.rpcGen
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return rpcEventMsg{gen: gen, ev: rpcclient.Event{Kind: rpcclient.EventConnectionClosed}}
		}
		batch := []rpcclient.Event{ev}
		const maxBatch = 32
		for len(batch) < maxBatch {
			select {
			case ev2, ok2 := <-ch:
				if !ok2 {
					goto done
				}
				batch = append(batch, ev2)
			default:
				goto done
			}
		}
	done:
		if len(batch) == 1 {
			return rpcEventMsg{gen: gen, ev: batch[0]}
		}
		return rpcBatchMsg{gen: gen, evs: batch}
	}
}

// handleRPCEvent mutates session state for an incoming agent event. Returns a
// tea.Cmd if the event finalized the assistant turn (AgentRunCompleted/Error)
// so the caller can autosave the session record.
func (a *App) handleRPCEvent(ev rpcclient.Event) tea.Cmd {
	var saveCmd tea.Cmd
	// High-frequency stream events skip immediate re-render; the 100ms tick
	// rebuilds chat when chatDirty while the turn is busy (~10 fps).
	skipRender := false

	switch ev.Kind {
	case rpcclient.EventReasoningDelta, rpcclient.EventMessageDelta,
		rpcclient.EventDone, rpcclient.EventStepUsage:
		skipRender = a.handleRPCStream(ev)

	case rpcclient.EventToolCallStart, rpcclient.EventToolCallDelta,
		rpcclient.EventToolCallCompleted, rpcclient.EventStepDone,
		rpcclient.EventRecoverableError, rpcclient.EventExecOutputChunk:
		skipRender = a.handleRPCTools(ev)

	case rpcclient.EventConnectionClosed, rpcclient.EventAgentRunCompleted,
		rpcclient.EventError, rpcclient.EventConnectionError:
		saveCmd = a.handleRPCTurnTerminal(ev)

	case rpcclient.EventPendingOps, rpcclient.EventTurnUsage,
		rpcclient.EventTurnTodos, rpcclient.EventTodosUpdated,
		rpcclient.EventInitialized, rpcclient.EventPermissionRequest,
		rpcclient.EventQuestionAsked, rpcclient.EventWorkflowStageStart,
		rpcclient.EventWorkflowStageDone, rpcclient.EventModeRoute:
		a.handleRPCChrome(ev)
	}

	if !skipRender {
		a.flushChat(true)
	}
	a.updateStatusHints()
	switch ev.Kind {
	case rpcclient.EventInitialized:
		return tea.Batch(saveCmd, a.awaitLSPWarmupCmd())
	case rpcclient.EventAgentRunCompleted:
		return tea.Batch(saveCmd, a.refreshLSPStatusCmd())
	default:
		return saveCmd
	}
}

// handleRPCStream handles reasoning/message deltas, per-step usage, and Done.
// Returns true when the caller should skip an immediate chat rebuild.
func (a *App) handleRPCStream(ev rpcclient.Event) (skipRender bool) {
	switch ev.Kind {
	case rpcclient.EventReasoningDelta:
		if delta := ev.Content; delta != "" {
			a.session.AppendAssistantReasoningDelta(delta)
			a.chat.SetStreamCursor(true)
			a.chatDirty = true
			return true
		}
	case rpcclient.EventMessageDelta:
		reasoning, message := a.reasoning.Feed(ev.Content)
		if reasoning != "" {
			a.session.AppendAssistantReasoningDelta(reasoning)
		}
		if message != "" {
			a.session.AppendAssistantDelta(message)
		}
		a.chat.SetStreamCursor(true)
		a.chatDirty = true
		return true
	case rpcclient.EventStepUsage:
		if ev.Usage != nil && ev.Usage.PromptTokens > 0 {
			tok := ev.Usage.PromptTokens
			isEst := strings.EqualFold(ev.Usage.Source, "estimate")
			// After reopen the bar shows last real PromptCtx. Don't let a
			// pessimistic estimate inflate it before provider usage arrives.
			if isEst && a.chrome.promptTokensUsed > 0 && !a.chrome.tokensEstimated {
				ceil := a.chrome.promptTokensUsed + a.chrome.promptTokensUsed/6 + 2048
				if tok > ceil {
					return false
				}
			}
			a.chrome.livePromptTokens = tok
			a.chrome.promptTokensUsed = tok
			a.chrome.tokensEstimated = isEst
			a.session.SetAssistantPromptCtx(tok)
			a.syncStatusBar()
			a.chatDirty = true
		}
	case rpcclient.EventDone:
		// Per LLM stream end (not end of user turn). Flush think-tag carry only.
		if a.reasoning.Carry != "" {
			if a.reasoning.InThink {
				a.session.AppendAssistantReasoningDelta(a.reasoning.Carry)
			} else {
				a.session.AppendAssistantDelta(a.reasoning.Carry)
			}
		}
		a.reasoning.Reset()
	}
	return false
}

// handleRPCTools handles tool-call lifecycle and recoverable errors.
func (a *App) handleRPCTools(ev rpcclient.Event) (skipRender bool) {
	switch ev.Kind {
	case rpcclient.EventToolCallStart:
		a.session.AppendToolBlock(state.ToolBlock{
			ID:        ev.ToolCallID,
			Name:      ev.ToolCallName,
			Status:    state.ToolBlockRunning,
			StartedAt: time.Now(),
		})
		a.statusBar.SetActiveTool(view.ToolDisplayName(ev.ToolCallName), "")
	case rpcclient.EventToolCallDelta:
		a.session.AppendToolArgsDelta(ev.ToolCallID, ev.ArgsDelta)
		if tb, ok := a.session.FindToolBlock(ev.ToolCallID); ok {
			a.statusBar.SetActiveTool(view.ToolDisplayName(tb.Name), view.ToolArgsPath(tb.Name, tb.ArgsRaw))
		}
		a.chatDirty = true
		return true
	case rpcclient.EventToolCallCompleted:
		if tb, ok := a.session.FindToolBlock(ev.ToolCallID); ok {
			if items := parseTodosFromTool(tb.Name, tb.ArgsRaw); len(items) > 0 {
				a.setTodos(items)
			}
		}
		status := state.ToolBlockCompleted
		switch {
		case strings.HasPrefix(ev.Content, "error: "):
			status = state.ToolBlockFailed
		case strings.HasPrefix(ev.Content, "skipped: "):
			status = state.ToolBlockSkipped
		}
		a.session.UpdateToolBlock(ev.ToolCallID, status, ev.Content, diagnosticsToState(ev.Diagnostics))
		a.statusBar.SetActiveTool("", "")
		// diagnostics present (even empty []) means SyncAndDiagnose ran → client is up
		if ev.Diagnostics != nil {
			a.chrome.lspStatus = "active"
			a.syncStatusBar()
		} else if isLSPToolName(ev.ToolCallName) {
			a.chrome.lspStatus = "active"
			a.syncStatusBar()
		}
	case rpcclient.EventStepDone:
		if ev.Content != "final" {
			// Keep mid-step narration in chronological segments (no truncate).
			if reason := stepDoneUserHint(ev.Content); reason != "" && !a.retryHintThisStep {
				a.session.AppendAssistantNotice(state.SystemKindRetry, reason)
			}
			a.retryHintThisStep = false
			a.session.FinalizeRunningTools()
		}
		a.statusBar.SetActiveTool("", "")
		a.stepTextLen = a.session.AssistantTextLen()
	case rpcclient.EventRecoverableError:
		if msg := strings.TrimSpace(ev.Content); msg != "" {
			if isSilentAgentRetry(msg) {
				// Empty-model retries are processed in the agent loop; don't spam chat.
				a.retryHintThisStep = true
				return false
			}
			if isContextCompactedNotice(msg) {
				a.session.AppendAssistantNotice(state.SystemKindInfo, "Контекст сжат — старая история суммаризирована")
				a.chatDirty = true
				return false
			}
			if isContextPressureNotice(msg) {
				a.session.AppendAssistantNotice(state.SystemKindInfo,
					"Бюджет промпта почти заполнен — скоро сжатие (это не % от полного окна модели; max_tokens резервирует место под ответ)")
				a.chatDirty = true
				return false
			}
			if isMaxStepsNotice(msg) {
				a.session.AppendAssistantNotice(state.SystemKindInfo, view.LocalizeRetryHint(msg))
				a.chatDirty = true
				return false
			}
			a.session.AppendAssistantNotice(state.SystemKindRetry, view.LocalizeRetryHint(msg))
			a.retryHintThisStep = true
			a.chatDirty = true
		}
	case rpcclient.EventExecOutputChunk:
		if chunk := ev.Content; chunk != "" {
			a.session.AppendRunningToolOutput(chunk)
			a.chatDirty = true
			return true
		}
	}
	return false
}

// handleRPCTurnTerminal handles end-of-turn / connection failure events.
func (a *App) handleRPCTurnTerminal(ev rpcclient.Event) tea.Cmd {
	switch ev.Kind {
	case rpcclient.EventConnectionClosed:
		if a.turn.ShowBusySpinner() {
			a.failAgentTurn()
			a.clearActiveCancel()
			a.session.AppendSystemNotice(state.SystemKindError, "Соединение с core оборвалось — ход прерван, не «готово»")
			a.statusBar.ClearError()
			a.chat.SetStreamCursor(false)
			a.chrome.livePromptTokens = 0
			a.layout()
			return a.persistSessionCmd()
		}
	case rpcclient.EventAgentRunCompleted:
		a.clearActiveCancel()
		if a.turnError != "" {
			a.reasoning.Reset()
			a.session.FinishAssistant()
			a.failAgentTurn()
			a.statusBar.ClearError()
			a.chat.SetStreamCursor(false)
			a.session.AppendSystemNotice(state.SystemKindError, a.turnError)
			a.turnError = ""
			a.chrome.livePromptTokens = 0
			a.layout()
			return a.persistSessionCmd()
		}
		if a.reasoning.Carry != "" {
			a.session.AppendAssistantDelta(a.reasoning.Carry)
		}
		a.reasoning.Reset()
		if a.chrome.promptTokensUsed > 0 {
			a.session.SetAssistantPromptCtx(a.chrome.promptTokensUsed)
		}
		a.session.FinishAssistant()
		queueCmd := a.finishAgentTurn()
		a.statusBar.ClearError()
		a.chrome.livePromptTokens = 0
		a.chat.SetStreamCursor(false)
		a.layout()
		return tea.Batch(a.persistSessionCmd(), queueCmd)
	case rpcclient.EventError:
		if msg := rpcEventErrorText(ev); msg != "" {
			a.turnError = msg
			// Error is shown in chat on turn complete — not in the status bar.
		}
	case rpcclient.EventConnectionError:
		a.failAgentTurn()
		a.clearActiveCancel()
		a.layout()
		msg := rpcEventErrorText(ev)
		if msg == "" {
			msg = "connection error"
		}
		a.statusBar.ClearError()
		a.chat.SetStreamCursor(false)
		a.session.AppendSystemNotice(state.SystemKindError, msg)
		return a.persistSessionCmd()
	}
	return nil
}

// handleRPCChrome handles pending ops, usage, todos, permissions, questions, workflows.
func (a *App) handleRPCChrome(ev rpcclient.Event) {
	switch ev.Kind {
	case rpcclient.EventPendingOps:
		if ev.PendingOps != nil {
			a.applyPendingOpsEvent(ev.PendingOps)
		}
	case rpcclient.EventTurnUsage:
		if ev.Usage != nil {
			a.recordTurnUsage(ev.Usage.PromptTokens, ev.Usage.CompletionTokens, ev.Usage.CostUSD)
			a.session.SetAssistantUsage(ev.Usage.PromptTokens, ev.Usage.CompletionTokens)
		}
	case rpcclient.EventTurnTodos, rpcclient.EventTodosUpdated:
		a.setTodos(ev.Todos)
		if ev.Kind == rpcclient.EventTurnTodos {
			a.noticeTurnStop(ev.StopReason, ev.OpenTodos, ev.Todos)
		}
	case rpcclient.EventInitialized:
		if ev.LSPStatus != "" {
			a.chrome.lspStatus = ev.LSPStatus
		}
		a.syncStatusBar()
	case rpcclient.EventPermissionRequest:
		if ev.PermReq != nil {
			a.handlePermissionRequestEvent(ev.PermReq)
		}
	case rpcclient.EventQuestionAsked:
		if len(ev.Questions) > 0 {
			items := make([]view.QuestionItem, len(ev.Questions))
			for i, q := range ev.Questions {
				items[i] = view.QuestionItem{Question: q.Question, Options: q.Options}
			}
			a.questionReqID = ev.ReqID
			a.questionModal = view.NewQuestionModal(items)
			a.layout()
		}
	case rpcclient.EventWorkflowStageStart, rpcclient.EventWorkflowStageDone:
		if ev.Stage != nil {
			a.handleWorkflowStageEvent(ev)
		}
	case rpcclient.EventModeRoute:
		if ev.ModeRoute != nil && ev.ModeRoute.To != "" {
			from := ev.ModeRoute.From
			if from == "" {
				from = "agent"
			}
			a.routeBadge = from + "→" + ev.ModeRoute.To
			a.showToast(a.routeBadge)
		}
	}
}

// applyPendingOpsEvent shows an applied-ops notice or arms the diff review
// for dry-run ops awaiting user confirmation.
func (a *App) applyPendingOpsEvent(po *rpcclient.PendingOpsPayload) {
	if po.Applied {
		if n := len(po.Ops); n > 0 {
			a.session.AppendSystemNotice(state.SystemKindSuccess,
				fmt.Sprintf("записано на диск: %d ops", n))
		}
		if len(po.Diff) > 0 {
			a.lastCommitDiff = append([]rpcclient.FileDiff(nil), po.Diff...)
			a.showDiffReview()
		}
	} else if len(po.Ops) > 0 || len(po.Diff) > 0 {
		a.review.Arm(po.Ops)
		if len(po.Diff) > 0 {
			a.lastCommitDiff = append([]rpcclient.FileDiff(nil), po.Diff...)
		}
		a.showDiffReview()
		a.syncActionBar()
		a.chat.SetMessages(a.session.Messages)
	}
	a.layout()
}

// showDiffReview (re)inserts the expanded diff block and resets the cursor.
func (a *App) showDiffReview() {
	if a.review.Shown() {
		a.session.RemoveDiff()
	}
	a.session.AddDiffFiles(a.buildDiffFiles())
	a.session.ExpandLastDiff()
	a.review.Show()
	a.syncDiffReviewCursor()
}

// handlePermissionRequestEvent answers session-allowed tools immediately and
// otherwise pushes the request into the permission FSM, presenting a modal
// when it became current.
func (a *App) handlePermissionRequestEvent(pr *rpcclient.PermissionRequestPayload) {
	if a.toolAllowedThisSession(pr.Tool) {
		if a.rpc != nil {
			a.rpc.RespondPermission(pr.ReqID, true)
		}
		return
	}
	kind := pr.Kind
	if kind == "" && pr.Tool == "lsp.install" {
		kind = "lsp.install"
	}
	req := state.PermRequest{
		ReqID:       pr.ReqID,
		Tool:        pr.Tool,
		Description: pr.Description,
		Kind:        kind,
	}
	if a.perms.Push(req) {
		a.permModal = view.NewPermissionModal(req.Tool, req.Description, req.Kind)
		a.layout()
	}
	// else: FIFO — shown after the current modal is answered.
}

// handleWorkflowStageEvent mirrors workflow stage progress into the progress
// panel and the chat transcript.
func (a *App) handleWorkflowStageEvent(ev rpcclient.Event) {
	st := ev.Stage
	if ev.Kind == rpcclient.EventWorkflowStageStart {
		if a.workflowProgress != nil {
			if !a.workflowProgress.Active() {
				a.workflowProgress.Begin(st.Name)
			}
			a.workflowProgress.StageStart(st.StageID)
		}
		a.session.AppendMessage(state.Message{
			Role:       state.RoleSystem,
			SystemKind: state.SystemKindInfo,
			Text: fmt.Sprintf("workflow «%s»: этап %s (попытка %d)",
				st.Name, st.StageID, st.Attempt),
		})
		a.layout()
		return
	}
	if a.workflowProgress != nil {
		a.workflowProgress.StageDone(st.StageID, st.Action)
	}
	marker := st.Marker
	if marker == "" {
		marker = "—"
	}
	a.session.AppendMessage(state.Message{
		Role:       state.RoleSystem,
		SystemKind: state.SystemKindInfo,
		Text: fmt.Sprintf("workflow «%s»: этап %s готов · marker=%s · %s · %dKB",
			st.Name, st.StageID, marker, st.Action, st.OutputKB),
	})
}

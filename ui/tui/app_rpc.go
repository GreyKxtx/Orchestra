package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func itoa(i int) string { return strconv.Itoa(i) }

func rpcEventErrorText(ev rpcclient.Event) string {
	if msg := strings.TrimSpace(ev.Err); msg != "" {
		return msg
	}
	return strings.TrimSpace(ev.Content)
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

// listenForEvents returns a Cmd that reads one event from the rpc channel.
func (a *App) listenForEvents() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	ch := a.rpc.Events()
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return rpcEventMsg{Kind: rpcclient.EventConnectionClosed}
		}
		return rpcEventMsg(ev)
	}
}

// handleRPCEvent mutates session state for an incoming agent event. Returns a
// tea.Cmd if the event finalized the assistant turn (AgentRunCompleted/Error)
// so the caller can autosave the session record.
func (a *App) handleRPCEvent(ev rpcclient.Event) tea.Cmd {
	var saveCmd tea.Cmd
	// High-frequency stream events (text/tool-args deltas) mutate session
	// state but skip the immediate re-render — the 100ms tick already calls
	// SetMessages while agentBusy and chatDirty, so we get a smooth 10 fps
	// view without rebuilding the entire chat content (and re-running the
	// glamour pipeline) for every single token. This was the source of
	// perceptible UI lag after the model "finished" — the queue of delta
	// events would still be draining for seconds.
	skipRender := false

	switch ev.Kind {
	case rpcclient.EventReasoningDelta:
		if delta := ev.Content; delta != "" {
			a.session.AppendAssistantReasoningDelta(delta)
			a.chat.SetStreamCursor(true)
			a.chatDirty = true
			skipRender = true
		}
	case rpcclient.EventMessageDelta:
		// Models that embed CoT in content still use <think>...</think>.
		// Dedicated reasoning_content arrives as EventReasoningDelta above.
		reasoning, message := a.reasoning.Feed(ev.Content)
		if reasoning != "" {
			a.session.AppendAssistantReasoningDelta(reasoning)
		}
		if message != "" {
			a.session.AppendAssistantDelta(message)
		}
		a.chat.SetStreamCursor(true)
		a.chatDirty = true
		skipRender = true
	case rpcclient.EventToolCallStart:
		a.session.AppendToolBlock(state.ToolBlock{
			ID:     ev.ToolCallID,
			Name:   ev.ToolCallName,
			Status: state.ToolBlockRunning,
		})
		a.statusBar.SetActiveTool(view.ToolDisplayName(ev.ToolCallName), "")
	case rpcclient.EventToolCallDelta:
		a.session.AppendToolArgsDelta(ev.ToolCallID, ev.ArgsDelta)
		// Update status bar with the latest path/preview as args stream in.
		if tb, ok := a.session.FindToolBlock(ev.ToolCallID); ok {
			a.statusBar.SetActiveTool(view.ToolDisplayName(tb.Name), view.ToolArgsPath(tb.Name, tb.ArgsRaw))
		}
		a.chatDirty = true
		skipRender = true
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
		if len(ev.Diagnostics) > 0 {
			a.lspStatus = "active"
			a.syncStatusBar()
		}
	case rpcclient.EventStepDone:
		if ev.Content != "final" {
			a.session.TruncateAssistantText(a.stepTextLen)
			if reason := stepDoneUserHint(ev.Content); reason != "" && !a.retryHintThisStep {
				a.session.AppendAssistantNotice(state.SystemKindRetry, reason)
			}
			a.retryHintThisStep = false
		}
		a.statusBar.SetActiveTool("", "")
		a.stepTextLen = a.session.AssistantTextLen()
	case rpcclient.EventRecoverableError:
		if msg := strings.TrimSpace(ev.Content); msg != "" {
			a.session.AppendAssistantNotice(state.SystemKindRetry, view.LocalizeRetryHint(msg))
			a.retryHintThisStep = true
			a.chatDirty = true
		}
	case rpcclient.EventExecOutputChunk:
		if chunk := ev.Content; chunk != "" {
			a.session.AppendRunningToolOutput(chunk)
			a.chatDirty = true
			skipRender = true
		}
	case rpcclient.EventConnectionClosed:
		if a.turn.ShowBusySpinner() {
			a.failAgentTurn()
			a.clearActiveCancel()
			a.session.AppendSystemNotice(state.SystemKindError, "соединение с core закрыто")
			a.statusBar.SetError("connection to core closed")
			a.chat.SetStreamCursor(false)
			a.livePromptTokens = 0
			a.layout()
			saveCmd = a.persistSessionCmd()
		}
	case rpcclient.EventStepUsage:
		if ev.Usage != nil && ev.Usage.PromptTokens > 0 {
			a.livePromptTokens = ev.Usage.PromptTokens
			a.promptTokensUsed = ev.Usage.PromptTokens
			a.session.SetAssistantPromptCtx(ev.Usage.PromptTokens)
			a.syncStatusBar()
			a.chatDirty = true
		}
	case rpcclient.EventDone:
		// EventDone fires at the end of EVERY LLM stream — i.e. once per agent
		// loop iteration, not once per user turn. Don't finalize the assistant
		// here: the agent may run more tool calls and stream more content
		// before the user turn actually ends. We only flush carry-over bytes
		// from `<think>` tag prefix detection (those are per-stream).
		if a.reasoning.Carry != "" {
			if a.reasoning.InThink {
				a.session.AppendAssistantReasoningDelta(a.reasoning.Carry)
			} else {
				a.session.AppendAssistantDelta(a.reasoning.Carry)
			}
		}
		a.reasoning.Reset()
	case rpcclient.EventAgentRunCompleted:
		// Real end of the user turn. Now we finalize.
		a.clearActiveCancel()
		if a.turnError != "" {
			a.reasoning.Reset()
			a.session.FinishAssistant()
			a.failAgentTurn()
			a.statusBar.SetError(a.turnError)
			a.chat.SetStreamCursor(false)
			a.session.AppendSystemNotice(state.SystemKindError, a.turnError)
			a.turnError = ""
			a.livePromptTokens = 0
			a.layout()
			saveCmd = a.persistSessionCmd()
			break
		}
		// Carry is always flushed to TEXT here — a stranded carry inside an
		// unclosed `<think>` block at end-of-run means the provider never
		// emitted `</think>`, so treating it as reasoning would hide it
		// forever. Better to surface the bytes as visible text.
		if a.reasoning.Carry != "" {
			a.session.AppendAssistantDelta(a.reasoning.Carry)
		}
		a.reasoning.Reset()
		if a.promptTokensUsed > 0 {
			a.session.SetAssistantPromptCtx(a.promptTokensUsed)
		}
		a.session.FinishAssistant()
		a.finishAgentTurn()
		a.statusBar.ClearError()
		a.livePromptTokens = 0
		a.chat.SetStreamCursor(false)
		a.layout()
		saveCmd = a.persistSessionCmd()
	case rpcclient.EventError:
		if msg := rpcEventErrorText(ev); msg != "" {
			a.turnError = msg
			a.statusBar.SetError(msg)
		}
	case rpcclient.EventConnectionError:
		a.failAgentTurn()
		a.clearActiveCancel()
		a.layout()
		msg := rpcEventErrorText(ev)
		if msg == "" {
			msg = "connection error"
		}
		a.statusBar.SetError(msg)
		a.chat.SetStreamCursor(false)
		a.session.AppendSystemNotice(state.SystemKindError, msg)
		saveCmd = a.persistSessionCmd()
	case rpcclient.EventPendingOps:
		if ev.PendingOps == nil {
			break
		}
		if ev.PendingOps.Applied {
			if n := len(ev.PendingOps.Ops); n > 0 {
				a.session.AppendSystemNotice(state.SystemKindSuccess,
					fmt.Sprintf("записано на диск: %d ops", n))
			}
			if len(ev.PendingOps.Diff) > 0 {
				a.lastCommitDiff = append([]rpcclient.FileDiff(nil), ev.PendingOps.Diff...)
				if a.diffShown {
					a.session.RemoveDiff()
				}
				a.session.AddDiffFiles(a.buildDiffFiles())
				a.diffShown = true
			}
		}
		a.layout()
	case rpcclient.EventTurnUsage:
		if ev.Usage != nil {
			a.recordTurnUsage(ev.Usage.PromptTokens, ev.Usage.CompletionTokens, ev.Usage.CostUSD)
			a.session.SetAssistantUsage(ev.Usage.PromptTokens, ev.Usage.CompletionTokens)
		}
	case rpcclient.EventTurnTodos, rpcclient.EventTodosUpdated:
		a.setTodos(ev.Todos)
	case rpcclient.EventInitialized:
		if ev.LSPStatus != "" {
			a.lspStatus = ev.LSPStatus
		}
		a.syncStatusBar()
	case rpcclient.EventPermissionRequest:
		if ev.PermReq != nil {
			if a.toolAllowedThisSession(ev.PermReq.Tool) {
				if a.rpc != nil {
					a.rpc.RespondPermission(true)
				}
				break
			}
			a.permModal = view.NewModal(ev.PermReq.Tool, ev.PermReq.Description)
			a.layout()
		}
	case rpcclient.EventQuestionAsked:
		if len(ev.Questions) > 0 {
			items := make([]view.QuestionItem, len(ev.Questions))
			for i, q := range ev.Questions {
				items[i] = view.QuestionItem{Question: q.Question, Options: q.Options}
			}
			a.questionModal = view.NewQuestionModal(items)
			a.layout()
		}
	case rpcclient.EventWorkflowStageStart:
		if ev.Stage != nil {
			if a.workflowProgress != nil {
				if !a.workflowProgress.Active() {
					a.workflowProgress.Begin(ev.Stage.Name)
				}
				a.workflowProgress.StageStart(ev.Stage.StageID)
			}
			a.session.AppendMessage(state.Message{
				Role:       state.RoleSystem,
				SystemKind: state.SystemKindInfo,
				Text: fmt.Sprintf("workflow «%s»: этап %s (попытка %d)",
					ev.Stage.Name, ev.Stage.StageID, ev.Stage.Attempt),
			})
			a.layout()
		}
	case rpcclient.EventWorkflowStageDone:
		if ev.Stage != nil {
			if a.workflowProgress != nil {
				a.workflowProgress.StageDone(ev.Stage.StageID, ev.Stage.Action)
			}
			marker := ev.Stage.Marker
			if marker == "" {
				marker = "—"
			}
			a.session.AppendMessage(state.Message{
				Role:       state.RoleSystem,
				SystemKind: state.SystemKindInfo,
				Text: fmt.Sprintf("workflow «%s»: этап %s готов · marker=%s · %s · %dKB",
					ev.Stage.Name, ev.Stage.StageID, marker, ev.Stage.Action, ev.Stage.OutputKB),
			})
		}
	}
	if !skipRender {
		a.chat.SetMessages(a.session.Messages)
	}
	a.updateStatusHints()
	return saveCmd
}

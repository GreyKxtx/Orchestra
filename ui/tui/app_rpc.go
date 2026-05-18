package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func itoa(i int) string { return strconv.Itoa(i) }

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
	case rpcclient.EventMessageDelta:
		// qwen3 / deepseek-r1 / etc emit chain-of-thought wrapped in
		// <think>...</think> tags inline with the answer. The splitter routes
		// think-tag content into Message.Reasoning and the rest into Message.Text.
		reasoning, message := a.reasoning.Feed(ev.Content)
		if reasoning != "" {
			a.session.AppendAssistantReasoningDelta(reasoning)
		}
		if message != "" {
			a.session.AppendAssistantDelta(message)
		}
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
		status := state.ToolBlockCompleted
		switch {
		case strings.HasPrefix(ev.Content, "error: "):
			status = state.ToolBlockFailed
		case strings.HasPrefix(ev.Content, "skipped: "):
			status = state.ToolBlockSkipped
		}
		a.session.UpdateToolBlock(ev.ToolCallID, status, ev.Content)
		a.statusBar.SetActiveTool("", "")
	case rpcclient.EventStepDone:
		// Text from non-final steps (tool_call, invalid retry, compaction) is
		// pre-tool commentary or scratch output — not the user-facing answer.
		// Drop it so only the final step's text remains in the message body.
		// Only "final" steps keep their text.
		if ev.Content != "final" {
			a.session.TruncateAssistantText(a.stepTextLen)
		}
		a.statusBar.SetActiveTool("", "")
		// Record where the next step's text starts so future truncations target
		// only that step's contribution.
		a.stepTextLen = a.session.AssistantTextLen()
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
		// Carry is always flushed to TEXT here — a stranded carry inside an
		// unclosed `<think>` block at end-of-run means the provider never
		// emitted `</think>`, so treating it as reasoning would hide it
		// forever. Better to surface the bytes as visible text.
		if a.reasoning.Carry != "" {
			a.session.AppendAssistantDelta(a.reasoning.Carry)
		}
		a.reasoning.Reset()
		a.session.FinishAssistant()
		a.agentBusy = false
		a.statusBar.SetAgentBusy(false)
		a.chat.SetAgentBusy(false)
		a.statusBar.ClearError()
		a.chat.SetStreamCursor(false)
		a.layout()
		saveCmd = a.persistSessionCmd()
	case rpcclient.EventError, rpcclient.EventConnectionError:
		a.agentBusy = false
		a.statusBar.SetAgentBusy(false)
		a.chat.SetAgentBusy(false)
		a.layout()
		a.statusBar.SetError(ev.Err)
		a.chat.SetStreamCursor(false)
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] " + ev.Err,
		})
		saveCmd = a.persistSessionCmd()
	case rpcclient.EventPendingOps:
		if ev.PendingOps != nil && !ev.PendingOps.Applied {
			a.pendingOps = ev.PendingOps
			a.layout()
		}
	case rpcclient.EventPermissionRequest:
		if ev.PermReq != nil {
			a.permModal = view.NewModal(ev.PermReq.Tool, ev.PermReq.Description)
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
				Role: state.RoleSystem,
				Text: "[workflow:" + ev.Stage.Name + "] → stage " + ev.Stage.StageID +
					" (attempt " + itoa(ev.Stage.Attempt) + ")",
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
				marker = "(no marker)"
			}
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "[workflow:" + ev.Stage.Name + "] ← stage " + ev.Stage.StageID +
					" done: marker=" + marker + ", action=" + ev.Stage.Action +
					", " + itoa(ev.Stage.OutputKB) + "KB out",
			})
		}
	}
	if !skipRender {
		a.chat.SetMessages(a.session.Messages)
	}
	a.updateStatusHints()
	return saveCmd
}

package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// enqueueMessage appends a user prompt to the FIFO queue shown while the agent is busy.
func (a *App) enqueueMessage(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.msgQueue = append(a.msgQueue, text)
}

func (a *App) queuedMessageCount() int {
	return len(a.msgQueue)
}

// startNextQueuedTurn pops the next queued message and kicks off an agent turn.
// Returns nil when the queue is empty or RPC is unavailable.
func (a *App) startNextQueuedTurn() tea.Cmd {
	if len(a.msgQueue) == 0 || a.rpc == nil {
		return nil
	}
	text := a.msgQueue[0]
	a.msgQueue = a.msgQueue[1:]
	return a.submitUserMessage(text)
}

// submitUserMessage is the shared Enter / queue-drain path for starting a turn.
func (a *App) submitUserMessage(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "/attach ") {
		path := strings.TrimSpace(strings.TrimPrefix(text, "/attach "))
		return a.cmdAttachFile(path)
	}
	if strings.EqualFold(text, "/attach") {
		a.showToast("/attach <path>")
		return nil
	}
	atts := a.takeStagedAttachments()
	if a.showWelcome {
		a.showWelcome = false
		a.chat.SetForceWelcome(false)
	}
	a.session.AppendMessage(state.Message{
		Role:        state.RoleUser,
		Text:        text,
		Attachments: atts,
		Mode:        a.cfg.Mode,
		Model:       a.cfg.Model,
	})
	a.history.Push(text)
	a.history.Reset()

	if cmd := a.maybeRunSkillOrWorkflow(text); cmd != nil {
		a.chat.SetMessages(a.session.Messages)
		return cmd
	}
	if cmd := a.maybeRunMemoryCommand(text); cmd != nil {
		a.chat.SetMessages(a.session.Messages)
		return cmd
	}

	a.session.StartAssistant(a.cfg.Mode, a.cfg.Model)
	a.reasoning.Reset()
	if a.subagents != nil {
		a.subagents.Reset()
	}
	a.stepTextLen = 0
	a.turnStartedAt = time.Now()
	a.chat.ScrollToBottom()
	a.chat.SetMessages(a.session.Messages)
	saveCmd := a.persistSessionCmd()

	if a.rpc == nil {
		// Echo fallback (tests without core).
		a.session.AppendAssistantDelta("echo: " + text)
		a.session.FinishAssistant()
		a.chat.SetMessages(a.session.Messages)
		return saveCmd
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.activeCancel = cancel
	a.beginAgentTurn()
	a.layout()
	a.updateStatusHints()

	// Snapshot everything the turn needs on the UI thread: the tea.Cmd runs
	// in its own goroutine and must never touch App fields (data race with
	// Update). Results come back exclusively through the RPC event stream.
	rpc := a.rpc
	sid := strings.TrimSpace(a.coreSessionID)
	if sid == "" {
		// Bind to the on-disk session id so we never silently start a
		// one-shot AgentRun that cannot restore history on the next message.
		sid = strings.TrimSpace(a.currentSessionID)
		a.coreSessionID = sid
	}
	opts := a.agentRunOptions()
	opts.Attachments = rpcAttachmentsFromState(atts)
	mode := a.cfg.Mode
	turnCmd := func() tea.Msg {
		if sid != "" {
			_ = rpc.SessionMessage(ctx, sid, text, mode, opts)
		} else {
			_ = rpc.AgentRun(ctx, text, mode, opts)
		}
		return nil
	}
	return tea.Batch(saveCmd, turnCmd)
}

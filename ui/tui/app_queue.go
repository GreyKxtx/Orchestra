package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
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

	a.openAssistantTurn()
	saveCmd := a.persistSessionCmd()

	if a.rpc == nil {
		// Echo fallback (tests without core).
		a.session.AppendAssistantDelta("echo: " + text)
		a.session.FinishAssistant()
		a.chat.SetMessages(a.session.Messages)
		return saveCmd
	}
	opts := a.agentRunOptions()
	opts.Attachments = rpcAttachmentsFromState(atts)
	return tea.Batch(saveCmd, a.launchTurn(text, opts))
}

// openAssistantTurn readies the chat for the answer that is about to stream.
func (a *App) openAssistantTurn() {
	a.session.StartAssistant(a.cfg.Mode, a.cfg.Model)
	a.reasoning.Reset()
	if a.subagents != nil {
		a.subagents.Reset()
	}
	a.stepTextLen = 0
	a.turnStartedAt = time.Now()
	a.chat.ScrollToBottom()
	a.chat.SetMessages(a.session.Messages)
}

// launchTurn starts the turn on the core — the turn FSM, its cancel — and
// returns the command that sends it: session.message in a session, agent.run
// without one. The tea.Cmd runs in its own goroutine and must never touch
// App fields (a data race with Update), so it takes everything it needs
// here; results come back through the RPC event stream alone.
func (a *App) launchTurn(text string, opts rpcclient.AgentRunOptions) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	a.activeCancel = cancel
	a.beginAgentTurn()
	a.layout()
	a.updateStatusHints()

	rpc := a.rpc
	sid := strings.TrimSpace(a.coreSessionID)
	if sid == "" {
		// Bind to the on-disk session id so we never silently start a
		// one-shot AgentRun that cannot restore history on the next message.
		sid = strings.TrimSpace(a.currentSessionID)
		a.coreSessionID = sid
	}
	mode := a.cfg.Mode
	return func() tea.Msg {
		if sid != "" {
			_ = rpc.SessionMessage(ctx, sid, text, mode, opts)
		} else {
			_ = rpc.AgentRun(ctx, text, mode, opts)
		}
		return nil
	}
}

// cmdResumeTurn is /resume: the session's interrupted turn goes on from its
// checkpoint — the same turn, its message and mode, with this TUI's consent
// flags (session.message resume). The checkpoint's user message is already
// in the chat, or comes back with the session.
func (a *App) cmdResumeTurn() tea.Cmd {
	turnID := strings.TrimSpace(a.resumableTurnID)
	if turnID == "" || a.rpc == nil || strings.TrimSpace(a.coreSessionID)+strings.TrimSpace(a.currentSessionID) == "" {
		a.session.AppendMessage(state.Message{
			Role:       state.RoleSystem,
			SystemKind: state.SystemKindInfo,
			Text:       "нечего продолжать: у этой сессии нет прерванного хода",
		})
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.resumableTurnID = ""
	a.openAssistantTurn()
	opts := a.agentRunOptions()
	opts.Resume = turnID
	return a.launchTurn("", opts)
}

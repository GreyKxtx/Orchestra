package tui

import (
	"context"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// isNoiseKey reports whether a key event is a terminal-side-effect artifact
// (cmd.exe emits stray NUL/Alt+NUL events around modifier presses that
// must not reach the textarea — otherwise NUL bytes pollute the value).
func isNoiseKey(km tea.KeyMsg) bool {
	if km.Type != tea.KeyRunes {
		return false
	}
	if len(km.Runes) == 0 {
		return true
	}
	for _, r := range km.Runes {
		if r != 0 {
			return false
		}
	}
	return true
}

// routeKey is the central key-handler dispatcher for the main chat view.
// Overlays (dialog stack, onboarding, command modal) get first dibs; if none
// claim the key, the per-key switch fires. Returns handled=false to let the
// outer Update fall through to textarea.
func (a *App) routeKey(m tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// Ctrl+C: copy selection to clipboard if active, otherwise quit.
	if m.String() == "ctrl+c" {
		if a.input.HasSelection() {
			lo, hi, _ := a.input.SelectionRange()
			runes := []rune(a.input.Value())
			if hi > len(runes) {
				hi = len(runes)
			}
			selected := string(runes[lo:hi])
			_ = clipboard.WriteAll(selected)
			a.input.ClearSelection()
			a.showToast("Скопировано")
			return a, nil, true
		}
		return a, tea.Quit, true
	}
	// Dialog stack takes priority over everything else.
	if len(a.dialogStack) > 0 {
		top := a.dialogStack[len(a.dialogStack)-1]
		updated, cmd := top.Update(m)
		a.dialogStack[len(a.dialogStack)-1] = updated
		return a, cmd, true
	}
	// Handle onboarding flow input next.
	if a.showOnboarding && a.onboarding != nil {
		next, cmd := a.updateOnboarding(m)
		return next, cmd, true
	}
	// Handle command modal input.
	if a.commandModal != nil && a.commandModal.Active() {
		next, cmd := a.updateCommandModal(m)
		return next, cmd, true
	}

	switch m.String() {
	case "ctrl+g":
		// Toggle mouse passthrough: when on, mouse reporting is disabled so the
		// terminal can handle native text selection. When off, restore our handlers.
		a.mousePassthrough = !a.mousePassthrough
		if a.mousePassthrough {
			a.updateStatusHints()
			a.showToast("Выделение текста · Ctrl+G для возврата")
			return a, tea.DisableMouse, true
		}
		a.updateStatusHints()
		a.showToast("Управление мышью восстановлено")
		return a, tea.EnableMouseCellMotion, true

	case "shift+left":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyLeft), true

	case "shift+right":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyRight), true

	case "ctrl+shift+left":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyCtrlLeft), true

	case "ctrl+shift+right":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyCtrlRight), true

	case "alt+shift+left":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyCtrlLeft), true

	case "alt+shift+right":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyCtrlRight), true

	case "ctrl+a":
		a.input.SelectAll()
		return a, nil, true
	case "ctrl+x":
		if a.input.HasSelection() {
			s := a.input.Cut()
			_ = clipboard.WriteAll(s)
			a.input.SyncHeight(5)
			a.layout()
			a.showToast("Вырезано")
		}
		return a, nil, true
	case "ctrl+v":
		txt, err := clipboard.ReadAll()
		if err == nil && txt != "" {
			a.input.Paste(txt)
			a.input.SyncHeight(5)
			a.layout()
		}
		return a, nil, true
	case "shift+home":
		a.input.SelectToLineStart()
		return a, nil, true
	case "shift+end":
		a.input.SelectToLineEnd()
		return a, nil, true
	case "ctrl+shift+home":
		a.input.SelectToDocStart()
		return a, nil, true
	case "ctrl+shift+end":
		a.input.SelectToDocEnd()
		return a, nil, true

	case "ctrl+home":
		a.input.ClearSelection()
		a.input.MoveCursorAbs(0)
		return a, nil, true
	case "ctrl+end":
		a.input.ClearSelection()
		runes := []rune(a.input.Value())
		a.input.MoveCursorAbs(len(runes))
		return a, nil, true

	case "shift+up":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyUp), true
	case "shift+down":
		if !a.input.HasSelection() {
			a.input.SetAnchor(a.input.CursorPos())
		}
		return a, a.sendKeyToTA(tea.KeyDown), true

	case "ctrl+left":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToStart()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyCtrlLeft), true
	case "ctrl+right":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToEnd()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyCtrlRight), true

	case "left":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToStart()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyLeft), true
	case "right":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToEnd()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyRight), true
	case "home":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToStart()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyHome), true
	case "end":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToEnd()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyEnd), true
	case "alt+left":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToStart()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyCtrlLeft), true
	case "alt+right":
		if a.input.HasSelection() {
			a.input.CollapseSelectionToEnd()
			return a, nil, true
		}
		return a, a.sendKeyToTA(tea.KeyCtrlRight), true

	case "up":
		if a.taskPanelOpen && a.taskPanel != nil {
			a.taskPanel.ScrollUp()
			a.layout()
			return a, nil, true
		}
		if a.paletteActive {
			a.slashPalette.CursorUp()
			return a, nil, true
		}
		if a.mentionActive {
			a.mentionPalette.CursorUp()
			return a, nil, true
		}
		w := a.input.WrapWidth()
		if a.input.VisualLineCount(w) > 1 {
			a.input.ClearSelection()
			return a, a.sendKeyToTA(tea.KeyUp), true
		}
		text := a.history.Up(a.input.Value())
		a.input.SetValue(text)
		return a, nil, true
	case "down":
		if a.taskPanelOpen && a.taskPanel != nil {
			a.taskPanel.ScrollDown()
			a.layout()
			return a, nil, true
		}
		if a.paletteActive {
			a.slashPalette.CursorDown()
			return a, nil, true
		}
		if a.mentionActive {
			a.mentionPalette.CursorDown()
			return a, nil, true
		}
		w := a.input.WrapWidth()
		if a.input.VisualLineCount(w) > 1 {
			a.input.ClearSelection()
			return a, a.sendKeyToTA(tea.KeyDown), true
		}
		if a.history.IsNavigating() {
			text := a.history.Down()
			a.input.SetValue(text)
			return a, nil, true
		}
	case "y":
		if a.permModal != nil {
			a.respondShellPermission(true, false, false)
			return a, nil, true
		}
	case "a":
		if a.permModal != nil {
			a.respondShellPermission(true, true, false)
			return a, nil, true
		}
	case "t":
		if a.permModal != nil {
			a.respondShellPermission(true, false, true)
			return a, nil, true
		}
		// Bare "t": Tasks when todos exist; else same cascade as Ctrl+T.
		// Only when input is empty — otherwise type normally.
		if a.inputEmpty() {
			if len(a.todos) > 0 {
				a.toggleTaskPanel()
				return a, nil, true
			}
			if a.handleCtrlTCascade() {
				return a, nil, true
			}
		}
	case "n":
		if a.permModal != nil {
			a.respondShellPermission(false, false, false)
			return a, nil, true
		}
	case "pgup":
		a.chat.ScrollUp(0)
		return a, nil, true
	case "pgdown":
		a.chat.ScrollDown(0)
		return a, nil, true
	case "ctrl+u":
		a.chat.ScrollUp(0)
		return a, nil, true
	case "ctrl+d":
		// With empty input + available diff → toggle commit diff (like bare "d").
		// Otherwise keep classic half-page scroll.
		if a.inputEmpty() && (len(a.lastCommitDiff) > 0 || a.session.HasDiff()) {
			a.showCommitDiff()
			return a, nil, true
		}
		a.chat.ScrollDown(0)
		return a, nil, true
	case "ctrl+t":
		a.handleCtrlTCascade()
		return a, nil, true
	case "ctrl+r":
		if a.toggleLastReasoning() {
			return a, nil, true
		}
	case "ctrl+k":
		if a.commandModal == nil {
			a.commandModal = view.NewPaletteModal(a.width, a.height)
		}
		a.commandModal.SetActive(true)
		return a, nil, true
	case "ctrl+o":
		if a.showOnboarding {
			return a, nil, true
		}
		if a.onboarding == nil {
			a.onboarding = view.NewOnboardingView(a.width, a.height)
		}
		a.onboarding.Step = view.OnboardingModel
		a.onboarding.LoadingModels = true
		a.showOnboarding = true
		endpoint := "http://localhost:1234"
		return a, fetchModelsCmd(endpoint), true
	case "esc":
		if a.input.HasSelection() {
			a.input.ClearSelection()
			return a, nil, true
		}
		if a.mentionActive {
			a.mentionActive = false
			a.layout()
			a.updateStatusHints()
			return a, nil, true
		}
		if a.paletteActive {
			a.paletteActive = false
			a.input.Reset()
			a.layout()
			a.updateStatusHints()
			return a, nil, true
		}
		if a.questionModal != nil {
			a.questionModal = nil
			a.input.Reset()
			a.updateStatusHints()
			a.layout()
			if a.rpc != nil {
				a.rpc.RespondQuestion(nil)
			}
			return a, nil, true
		}
		if a.permModal != nil {
			a.respondShellPermission(false, false, false)
			return a, nil, true
		}
		// While a long-running RPC is in flight, Esc cancels it. The local
		// context cancel unblocks the rpcclient.Call which auto-sends
		// `$/cancelRequest` to the server — the server then cancels its
		// per-request ctx so agent.run / workflow.run / skill.invoke unwind
		// promptly. The visible "busy" state clears via the result/error
		// event path (EventAgentRunCompleted, workflowResultMsg, etc.).
		if a.turn.CanCancel() && a.activeCancel != nil {
			a.clearActiveCancel()
			a.session.AppendMessage(state.Message{
				Role:       state.RoleSystem,
				SystemKind: state.SystemKindInfo,
				Text:       "ход отменён",
			})
			a.chat.SetMessages(a.session.Messages)
			return a, nil, true
		}
		a.input.Reset()
		return a, nil, true
	case "tab":
		if a.mentionActive {
			if sel := a.mentionPalette.Selected(); sel != "" {
				a.input.SetValue(replaceLastMention(a.input.Value(), sel))
				a.mentionActive = false
				a.syncPalette()
				a.layout()
				a.updateStatusHints()
			}
			return a, nil, true
		}
		// Cycle through agent modes (build → ask → plan → build).
		a.cycleAgentMode()
		return a, nil, true
	case "d":
		// Bare "d" only when input is empty — otherwise type normally.
		if a.inputEmpty() && (len(a.lastCommitDiff) > 0 || a.session.HasDiff()) {
			a.showCommitDiff()
			return a, nil, true
		}
	case "backspace":
		a.input.DeleteBackward()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
	case "delete":
		a.input.DeleteForward()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
	case "shift+enter", "ctrl+j":
		// shift+enter works in Windows Terminal / xterm; ctrl+j is the
		// universal fallback (literal newline char \n) for terminals
		// like cmd.exe that don't distinguish Shift+Enter from Enter.
		a.input.InsertNewline()
		a.input.SyncHeight(5)
		a.layout()
		return a, nil, true
	case "enter":
		return a.handleEnter()
	}
	// Clear selection when a non-shift navigation key falls through to textarea.
	// left/right/home/end/ctrl|alt+left|right are handled above when a selection
	// is active (collapse to edge). Remaining keys clear then fall through.
	switch m.String() {
	case "up", "down":
		a.input.ClearSelection()
	}
	return a, nil, false
}

// handleEnter processes the Enter key in the main chat view — completes a
// mention/palette suggestion, or submits the current input as a new user
// message and kicks off an agent run.
func (a *App) handleEnter() (tea.Model, tea.Cmd, bool) {
	if a.questionModal != nil {
		answer := strings.TrimSpace(a.input.Value())
		if answer == "" {
			return a, nil, true
		}
		done := a.questionModal.Advance(answer)
		a.input.Reset()
		if done {
			answers := append([]string(nil), a.questionModal.Answers...)
			a.questionModal = nil
			a.layout()
			a.updateStatusHints()
			if a.rpc != nil {
				a.rpc.RespondQuestion(answers)
			}
		}
		return a, nil, true
	}
	if a.mentionActive {
		if sel := a.mentionPalette.Selected(); sel != "" {
			a.input.SetValue(replaceLastMention(a.input.Value(), sel))
		}
		a.mentionActive = false
		a.syncPalette()
		a.layout()
		a.updateStatusHints()
		return a, nil, true
	}
	if a.paletteActive {
		selectedCmd := a.slashPalette.Selected()
		a.paletteActive = false
		a.input.Reset()
		a.layout()
		a.updateStatusHints()
		cmd := a.executePaletteCmd(selectedCmd)
		return a, cmd, true
	}
	if a.turn.BlocksSubmit() {
		return a, nil, true
	}
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil, true
	}
	// Dismiss welcome screen on first message.
	if a.showWelcome {
		a.showWelcome = false
		a.chat.SetForceWelcome(false)
	}
	a.session.AppendMessage(state.Message{
		Role:  state.RoleUser,
		Text:  text,
		Mode:  a.cfg.Mode,
		Model: a.cfg.Model,
	})
	a.history.Push(text)
	a.history.Reset()
	a.input.Reset()

	// Intercept skill/workflow commands BEFORE the generic agent.run path.
	// "/workflows" and "/skills" (no args) list available items.
	// "/workflow <name> <args>" runs the named workflow with the rest as
	// arguments. Same for "/skill <name> <args>". Anything else falls
	// through to the regular agent.run flow.
	if cmd := a.maybeRunSkillOrWorkflow(text); cmd != nil {
		a.chat.SetMessages(a.session.Messages)
		return a, cmd, true
	}

	a.session.StartAssistant(a.cfg.Mode, a.cfg.Model)
	a.reasoning.Reset()
	a.stepTextLen = 0
	a.turnStartedAt = time.Now()
	a.chat.ScrollToBottom()
	a.chat.SetMessages(a.session.Messages)
	saveCmd := a.persistSessionCmd()
	if a.rpc != nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.activeCancel = cancel
		a.beginAgentTurn()
		a.layout()
		a.updateStatusHints()
		go func(query, mode string) {
			_ = a.runAgentTurn(ctx, query, mode)
		}(text, a.cfg.Mode)
		return a, saveCmd, true
	}
	// Echo fallback (tests).
	a.session.AppendAssistantDelta("echo: " + text)
	a.session.FinishAssistant()
	a.chat.SetMessages(a.session.Messages)
	return a, saveCmd, true
}

// isPrintableKey reports whether a key message represents printable input
// (regular typing or a bracketed paste from the terminal).
func isPrintableKey(km tea.KeyMsg) bool {
	return km.Type == tea.KeyRunes && len(km.Runes) > 0
}

// inputEmpty reports whether the composer has no text (hotkeys may steal keys).
func (a *App) inputEmpty() bool {
	return strings.TrimSpace(a.input.Value()) == ""
}

// handleCtrlTCascade: close open Tasks; else expand tools; else toggle diff;
// else open Tasks. Tools/diff stay reachable even when todos exist.
func (a *App) handleCtrlTCascade() bool {
	if a.taskPanelOpen {
		a.toggleTaskPanel()
		return true
	}
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		m := a.session.Messages[i]
		if m.Role == state.RoleAssistant && len(m.ToolBlocks) > 0 {
			key := view.MessageKey(m)
			a.chat.ExpandTurn(key)
			a.session.Messages[i].ToolsExpanded = a.chat.IsTurnExpanded(key)
			a.chat.SetMessages(a.session.Messages)
			a.layout()
			a.updateStatusHints()
			return true
		}
	}
	if a.session.ToggleLastDiff() {
		a.diffShown = a.session.HasDiff()
		a.chat.SetMessages(a.session.Messages)
		a.layout()
		a.updateStatusHints()
		return true
	}
	if len(a.todos) > 0 {
		a.toggleTaskPanel()
		return true
	}
	return false
}

// toggleLastReasoning expands/collapses CoT on the newest assistant message.
func (a *App) toggleLastReasoning() bool {
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		m := &a.session.Messages[i]
		if m.Role != state.RoleAssistant || strings.TrimSpace(m.Reasoning) == "" {
			continue
		}
		m.ReasoningExpanded = !m.ReasoningExpanded
		a.chat.InvalidateMessage(view.MessageKey(*m))
		a.chat.SetMessages(a.session.Messages)
		a.layout()
		return true
	}
	return false
}

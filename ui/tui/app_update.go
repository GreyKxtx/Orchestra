package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
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

// Update routes incoming messages to the appropriate sub-handler.
//
// The big switch covers every tea.Msg variant we care about:
//   - WindowSizeMsg / tickMsg                         — layout + animation
//   - modelsLoadedMsg / onboardingDoneMsg / rpcSpawnedMsg — onboarding/spawn flow
//   - DialogResultMsg / ModelsLoadedMsg / settingsSavedMsg — dialog stack flow
//   - MouseMsg                                        — wheel scroll
//   - KeyMsg                                          — main keyboard dispatcher
//   - rpcEventMsg / applyResultMsg                    — agent streaming + apply
//
// Anything not handled here falls through to the textarea.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Drop terminal-side-effect noise events (cmd.exe emits stray NUL key
	// presses around modifier keys; they pollute the textarea value).
	if km, ok := msg.(tea.KeyMsg); ok && isNoiseKey(km) {
		return a, nil
	}

	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.layout()
		// Rebuild chat content at the new width so existing user/assistant
		// messages re-wrap to the new viewport size. layout() resizes the
		// viewport but does NOT regenerate the rendered content — without
		// this call, messages keep their pre-resize width.
		if a.initialized {
			a.chat.SetMessages(a.session.Messages)
		}
		return a, nil

	case tickMsg:
		a.spinFrame++
		// Cursor blink at ~500ms (every 5 ticks) so the textarea cursor doesn't
		// strobe; spinner runs at the full 10 fps tick rate.
		blinkChanged := false
		if a.spinFrame%5 == 0 {
			a.cursorBlink = !a.cursorBlink
			blinkChanged = true
		}
		a.statusBar.AdvanceSpin()
		a.chat.SetSpinFrame(a.spinFrame)
		// Only rebuild chat on a tick when something animatable is in flight:
		//   - a streaming delta landed since last tick (chatDirty)
		//   - we have a running tool / streaming turn that needs the spinner
		//     frame to advance visibly
		//   - the streaming-cursor blink state just flipped
		if a.agentBusy && (a.chatDirty || a.session.HasRunningTool() || blinkChanged) {
			a.chat.SetStreamCursor(a.cursorBlink)
			a.chat.SetMessages(a.session.Messages)
			a.chatDirty = false
		}
		if a.toastTick > 0 {
			a.toastTick--
			if a.toastTick == 0 {
				a.toastText = ""
			}
		}
		return a, tickCmd()

	case modelsLoadedMsg:
		if a.onboarding != nil {
			a.onboarding.LoadingModels = false
			if m.err != nil {
				a.onboarding.ModelError = "LM Studio недоступен: " + m.err.Error()
			} else {
				a.onboarding.Models = m.models
				a.onboarding.ModelError = ""
			}
		}
		return a, nil

	case onboardingDoneMsg:
		a.showOnboarding = false
		cfg, err := config.Load(m.configPath)
		if err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to load config: " + err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.cfg.Model = cfg.LLM.Model
		a.statusBar.SetModel(cfg.LLM.Model)
		a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
		a.chat.SetWelcomeInfo(a.buildWelcomeInfo())
		binary := a.cfg.Binary
		workspaceRoot := a.cfg.WorkspaceRoot
		projectID := a.cfg.ProjectID
		return a, func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			client, err := rpcclient.Spawn(ctx, rpcclient.Config{
				Binary:        binary,
				WorkspaceRoot: workspaceRoot,
				ProjectID:     projectID,
			})
			return rpcSpawnedMsg{client: client, cancel: cancel, err: err}
		}

	case rpcSpawnedMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to connect to core: " + m.err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.rpc = m.client
		a.rpcCancel = m.cancel
		return a, a.listenForEvents()

	case view.DialogResultMsg:
		return a.handleDialogResult(m)

	case view.ModelsLoadedMsg:
		// Forward to the ModelDialog at the top of the stack, if any.
		if len(a.dialogStack) > 0 {
			if md, ok := a.dialogStack[len(a.dialogStack)-1].(*view.ModelDialog); ok {
				if m.Err != "" {
					md.SetLoadError(m.Err)
				} else {
					md.SetModels(m.Models)
				}
			}
		}
		return a, nil

	case settingsSavedMsg:
		// After /provider or /model flow saved, refresh app state and respawn
		// the core subprocess so the new model takes effect now.
		return a, a.applySavedSettings(m)

	case tea.MouseMsg:
		switch {
		case m.Button == tea.MouseButtonWheelUp && m.Action == tea.MouseActionPress:
			a.chat.ScrollUp(3)
			return a, nil
		case m.Button == tea.MouseButtonWheelDown && m.Action == tea.MouseActionPress:
			a.chat.ScrollDown(3)
			return a, nil
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			// In passthrough mode mouse reporting is disabled — this event won't arrive.
			inputH := a.input.Inner().Height()
			if inputH < 1 {
				inputH = 1
			}
			if m.Y < a.inputRowY || m.Y >= a.inputRowY+inputH {
				// Click is outside the input box — check if it's in the chat viewport.
				topY := a.chatTopY
				if topY <= 0 {
					topY = chatVerticalPad
				}
				if m.Y >= topY && m.Y < a.inputRowY && !a.agentBusy && !a.showWelcome {
					contentY := a.chat.ViewportYOffset() + (m.Y - topY)
					role, text, ok := a.chat.MessageAtContentY(contentY)
					if ok && text != "" {
						if role == state.RoleUser {
							a.pushDialog(view.NewMessageActionDialog(text, true))
						} else if role == state.RoleAssistant {
							_ = clipboard.WriteAll(text)
							a.showToast("Скопировано")
						}
					}
				}
				return a, nil
			}
			rowOff := m.Y - a.inputRowY
			charPos := a.mouseXYToAbsolutePos(m.X, rowOff)
			if m.Shift {
				a.input.ExtendSelectionTo(charPos)
				a.mouseLastClickAt = time.Time{} // reset double-click chain
				a.mouseClickCount = 0
				return a, nil
			}
			now := time.Now()
			absDiff := charPos - a.mouseLastClickPos
			if absDiff < 0 {
				absDiff = -absDiff
			}
			if now.Sub(a.mouseLastClickAt) <= 400*time.Millisecond && absDiff <= 2 {
				a.mouseClickCount++
				if a.mouseClickCount > 3 {
					a.mouseClickCount = 3
				}
			} else {
				a.mouseClickCount = 1
			}
			a.mouseLastClickAt = now
			a.mouseLastClickPos = charPos

			switch a.mouseClickCount {
			case 1:
				a.input.ClearSelection()
				a.input.SetAnchor(charPos)
				a.input.SetMouseCaret(charPos)
				a.mouseDown = true
			case 2:
				lo, hi := a.input.WordRange(charPos)
				if lo != hi {
					a.input.SetAnchor(lo)
					a.input.MoveCursorAbs(hi)
				}
			case 3:
				lo, hi := a.input.LineRange(charPos)
				if lo != hi {
					a.input.SetAnchor(lo)
					a.input.MoveCursorAbs(hi)
				}
			}
			return a, nil
		case m.Action == tea.MouseActionRelease:
			if a.mouseDown {
				a.mouseDown = false
				caret := a.input.MouseCaret()
				a.input.MoveCursorAbs(caret)
				a.input.ClearMouseCaret()
				if lo, hi, ok := a.input.SelectionRange(); ok && lo == hi {
					a.input.ClearSelection()
				}
			}
			return a, nil
		case m.Action == tea.MouseActionMotion:
			if a.mouseDown {
				inputH := a.input.Inner().Height()
				if inputH < 1 {
					inputH = 1
				}
				if m.Y >= a.inputRowY && m.Y < a.inputRowY+inputH {
					rowOff := m.Y - a.inputRowY
					charPos := a.mouseXYToAbsolutePos(m.X, rowOff)
					a.input.SetMouseCaret(charPos)
				} else if m.Y < a.inputRowY {
					// Above input — clamp to position 0 of first row.
					a.input.SetMouseCaret(0)
				} else {
					// Below input — clamp to end of value.
					runes := []rune(a.input.Value())
					a.input.SetMouseCaret(len(runes))
				}
			}
			return a, nil
		case m.Button == tea.MouseButtonRight && m.Action == tea.MouseActionPress:
			if a.input.HasSelection() {
				lo, hi, _ := a.input.SelectionRange()
				runes := []rune(a.input.Value())
				if hi > len(runes) {
					hi = len(runes)
				}
				_ = clipboard.WriteAll(string(runes[lo:hi]))
				a.showToast("Скопировано")
			}
			return a, nil
		}
		return a, nil

	case tea.KeyMsg:
		if next, cmd, handled := a.routeKey(m); handled {
			return next, cmd
		}

	case rpcEventMsg:
		saveCmd := a.handleRPCEvent(rpcclient.Event(m))
		listenCmd := a.listenForEvents()
		return a, tea.Batch(saveCmd, listenCmd)

	case applyResultMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[apply failed] " + m.err.Error()})
		} else {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: fmt.Sprintf("[applied %d ops]", m.count)})
		}
		a.chat.SetMessages(a.session.Messages)
		return a, nil

	case systemMsgMsg:
		return a, a.handleSystemMsg(m)

	case workflowResultMsg:
		return a, a.handleWorkflowResult(m)

	case skillResultMsg:
		return a, a.handleSkillResult(m)
	}

	// Type-to-replace: printable input while a selection is active replaces
	// the selection rather than appending alongside it.
	if km, ok := msg.(tea.KeyMsg); ok && a.input.HasSelection() && isPrintableKey(km) {
		a.input.ReplaceSelection(string(km.Runes))
		a.input.SyncHeight(5)
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
		return a, nil
	}
	// Forward all messages to textarea (default fall-through for unhandled keys).
	innerTA := a.input.Inner()
	updatedTA, taCmd := innerTA.Update(msg)
	*innerTA = updatedTA
	if _, isKey := msg.(tea.KeyMsg); isKey {
		// Any key that reaches textarea clears selection (user typed a character).
		a.input.ClearSelection()
		// Resync visible height — every keystroke may have changed the
		// number of soft-wrapped visual rows (typing past width boundary).
		a.input.SyncHeight(5)
		a.syncPalette()
		a.updateStatusHints()
		a.layout()
	}
	return a, taCmd
}

// sendKeyToTA forwards a synthetic key event directly to the textarea.
func (a *App) sendKeyToTA(kt tea.KeyType) tea.Cmd {
	innerTA := a.input.Inner()
	updated, cmd := innerTA.Update(tea.KeyMsg{Type: kt})
	*innerTA = updated
	return cmd
}

// showToast displays a temporary notification for ~1.5 seconds (15 ticks at 10fps).
func (a *App) showToast(text string) {
	a.toastText = text
	a.toastTick = 15
}

// mouseXYToAbsolutePos converts (screenX, visualRowOffset) to an absolute
// rune index. visualRowOffset is 0 for the topmost input row, 1 for the
// second visual row, etc. — covers BOTH logical-line breaks and soft-wrap
// continuations. Uses the same word-aware grid as the renderer so a click
// lands on the rune the user actually sees under the cursor.
func (a *App) mouseXYToAbsolutePos(screenX, rowOffset int) int {
	colOffset := screenX - a.inputColX
	if colOffset < 0 {
		colOffset = 0
	}
	if rowOffset < 0 {
		rowOffset = 0
	}
	wrapW := a.input.WrapWidth()
	if wrapW < 1 {
		wrapW = 80
	}
	rows := a.input.VisualRows(wrapW)
	if len(rows) == 0 {
		return 0
	}
	if rowOffset >= len(rows) {
		return len([]rune(a.input.Value()))
	}
	r := rows[rowOffset]
	rowLen := len(r.Runes)
	c := colOffset
	if c > rowLen {
		c = rowLen
	}
	return r.AbsStart + c
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
		a.input.ClearSelection()
		return a, a.sendKeyToTA(tea.KeyCtrlLeft), true
	case "ctrl+right":
		a.input.ClearSelection()
		return a, a.sendKeyToTA(tea.KeyCtrlRight), true

	case "up":
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
			a.permModal = nil
			a.updateStatusHints()
			if a.rpc != nil {
				a.rpc.RespondPermission(true)
			}
			return a, nil, true
		}
	case "n":
		if a.permModal != nil {
			a.permModal = nil
			a.updateStatusHints()
			if a.rpc != nil {
				a.rpc.RespondPermission(false)
			}
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
		a.chat.ScrollDown(0)
		return a, nil, true
	case "ctrl+t":
		// Toggle expand/collapse on the most recent assistant turn that
		// has tool blocks — same UX as opencode's "view subagents" key.
		// Keyed by StartedAt so /clear and RemoveDiff don't desync state.
		for i := len(a.session.Messages) - 1; i >= 0; i-- {
			m := a.session.Messages[i]
			if m.Role == state.RoleAssistant && len(m.ToolBlocks) > 0 {
				a.chat.ExpandTurn(view.MessageKey(m))
				a.chat.SetMessages(a.session.Messages)
				break
			}
		}
		return a, nil, true
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
			a.permModal = nil
			a.updateStatusHints()
			if a.rpc != nil {
				a.rpc.RespondPermission(false)
			}
			return a, nil, true
		}
		// While a long-running RPC is in flight, Esc cancels it. The local
		// context cancel unblocks the rpcclient.Call which auto-sends
		// `$/cancelRequest` to the server — the server then cancels its
		// per-request ctx so agent.run / workflow.run / skill.invoke unwind
		// promptly. The visible "busy" state clears via the result/error
		// event path (EventAgentRunCompleted, workflowResultMsg, etc.).
		if a.agentBusy && a.activeCancel != nil {
			a.clearActiveCancel()
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: "[cancelled by user]",
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
	case "a":
		if a.pendingOps != nil && a.rpc != nil {
			rawOps := a.pendingOps.Ops
			count := len(a.pendingOps.Ops)
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
			a.layout()
			a.updateStatusHints()
			rpc := a.rpc
			return a, func() tea.Msg {
				return applyResultMsg{err: rpc.ApplyOps(context.Background(), rawOps), count: count}
			}, true
		}
	case "d":
		if a.pendingOps != nil {
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			} else {
				content := a.buildDiffContent()
				a.session.AddDiff(content)
				a.diffShown = true
			}
			a.chat.SetMessages(a.session.Messages)
			return a, nil, true
		}
	case "x":
		if a.pendingOps != nil {
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
			a.layout()
			a.updateStatusHints()
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
	switch m.String() {
	case "left", "right", "ctrl+left", "ctrl+right", "alt+left", "alt+right", "home", "end", "up", "down":
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
	if a.agentBusy {
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
		a.agentBusy = true
		a.statusBar.SetAgentBusy(true)
		a.chat.SetAgentBusy(true)
		ctx, cancel := context.WithCancel(context.Background())
		a.activeCancel = cancel
		a.layout()
		a.updateStatusHints()
		go func(query, mode string) {
			_ = a.rpc.AgentRun(ctx, query, mode, a.agentRunOptions())
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

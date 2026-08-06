package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

const statusBarScreenRows = 2

// chatSidePad is the symmetric horizontal margin applied to the chat
// scrollback area only — so individual messages don't hug the terminal edge.
// The input box, action bar, palette and status bar render at the full
// terminal width: they have their own internal padding / border / bg, and
// wrapping them in an outer style would re-flow already laid-out cells and
// visually shred multi-line, bordered, bg-styled blocks (the "sliced" input
// bug). Welcome view has its own centered layout — unaffected.
const chatSidePad = 1

// chatVerticalPad is the blank-line gutter above and below the chat scrollback
// so the first message doesn't touch the top edge and the last message has
// breathing room from the input box below.
const chatVerticalPad = 1

// padChat adds chatSidePad cells of horizontal margin around the rendered
// chat viewport so messages don't hug the terminal edges. Multi-line content
// already laid out by the viewport keeps its internal width; we just add a
// blank gutter on each side.
func (a *App) padChat(content string) string {
	return lipgloss.NewStyle().
		PaddingTop(chatVerticalPad).
		PaddingBottom(chatVerticalPad).
		PaddingLeft(chatSidePad).
		PaddingRight(chatSidePad).
		Render(content)
}

// View renders the full screen layout — top-of-screen dispatcher that picks
// between dialog overlay, onboarding wizard, welcome layout, and the main
// chat layout (chat scroll + action bar + palettes + input + status bar).
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}

	// Dialog stack overlays everything (Provider/Model/Settings).
	if len(a.dialogStack) > 0 {
		top := a.dialogStack[len(a.dialogStack)-1]
		return top.Render(a.width, a.height)
	}

	// Onboarding overlays everything else.
	if a.showOnboarding && a.onboarding != nil {
		a.onboarding.SetScreenSize(a.width, a.height)
		return a.onboarding.Render()
	}

	// OpenCode-style welcome layout: centered logo + input box + tip.
	if a.showWelcome {
		return a.renderWelcomeView()
	}

	// Chat layout: chat + input box + status bar.
	parts := []string{a.padChat(a.chat.Render())}
	parts = append(parts, a.renderPalettesAndInput()...)
	return a.overlayModals(strings.Join(parts, "\n"))
}

func (a *App) renderPalettesAndInput() []string {
	var parts []string
	if a.paletteActive && len(a.slashPalette.Items) > 0 {
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(a.slashPalette.Render()))
	}
	if a.mentionActive && len(a.mentionPalette.Items) > 0 {
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(a.mentionPalette.Render()))
	}
	if a.workflowProgress != nil && a.workflowProgress.Active() {
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(a.workflowProgress.Render()))
	}
	if a.questionModal != nil {
		parts = append(parts, a.questionModal.Render())
		parts = append(parts, a.renderTaskArea())
		parts = append(parts, a.renderChatInputBox())
	} else if a.permModal != nil {
		parts = append(parts, a.renderTaskArea())
		parts = append(parts, a.permModal.Render())
	} else {
		parts = append(parts, a.renderTaskArea())
		parts = append(parts, a.renderChatInputBox())
	}
	parts = append(parts, a.statusBar.Render())
	return parts
}

func (a *App) renderTaskArea() string {
	if a.taskPanel == nil || len(a.todos) == 0 {
		return ""
	}
	panel := a.taskPanel.RenderAboveInput()
	if panel == "" {
		return ""
	}
	return lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(panel)
}

func (a *App) overlayModals(screen string) string {
	if a.commandModal != nil && a.commandModal.Active() {
		a.commandModal.SetScreenSize(a.width, a.height)
		return a.commandModal.Render()
	}
	if toast := a.renderToast(); toast != "" {
		lines := strings.Split(screen, "\n")
		toastLines := strings.Split(toast, "\n")
		for i, tl := range toastLines {
			if i < len(lines) {
				lines[i] = tl
			}
		}
		return strings.Join(lines, "\n")
	}
	return screen
}

// renderChatInputBox renders the same grey-bg ▌ box that the welcome screen
// uses, scaled to the current terminal width minus a one-cell gutter on each
// side. Resizing the terminal grows/shrinks the box automatically; clamped
// to a 40-cell minimum so narrow terminals don't crush the textarea below
// usable size.
func (a *App) renderChatInputBox() string {
	w := a.width - 2*chatSidePad
	if w < 40 {
		w = 40
	}
	box := a.renderInputBox(w)
	return lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(box)
}

// layout recomputes child sizes based on current width/height. Called on
// WindowSize and whenever a layout-affecting state changes (agent busy,
// permission modal, palette active).
func (a *App) layout() {
	chatW := a.width - 2*chatSidePad
	if chatW < 1 {
		chatW = 1
	}
	a.statusBar.SetWidth(a.width)
	if a.commandModal != nil {
		a.commandModal.SetScreenSize(a.width, a.height)
	}

	actionBarRows := 0
	progressRows := 0
	// Task panel overlays chat — does not consume layout rows (avoids input jumping).
	if a.workflowProgress != nil && a.workflowProgress.Active() {
		progressRows = 1
	}
	paletteRows := 0
	if a.paletteActive && len(a.slashPalette.Items) > 0 {
		n := len(a.slashPalette.Items)
		if n > 6 {
			n = 6
		}
		paletteRows = n // SplitBorder has no top/bottom rows
	} else if a.mentionActive && len(a.mentionPalette.Items) > 0 {
		n := len(a.mentionPalette.Items)
		if n > 6 {
			n = 6
		}
		paletteRows = n
	}
	// Boxed input: pad-top + textarea + gap + modeline + pad-bottom = 5 + taH rows.
	inputRows := 5
	if !a.showWelcome {
		taH := a.input.Inner().Height()
		if taH < 1 {
			taH = 1
		}
		inputRows = taH + 5
	}
	modalRows := 0
	if a.questionModal != nil {
		modalRows = 4
		a.questionModal.SetSize(a.width)
	} else if a.permModal != nil {
		inputRows = 0 // modal replaces input
		modalRows = 5
		a.permModal.SetSize(a.width)
	}
	taskRows := 0
	if a.taskPanel != nil && len(a.todos) > 0 {
		taskRows = a.taskPanel.VisibleRowsAboveInput()
	}
	const statusBarRows = statusBarScreenRows
	chatHeight := a.height - statusBarRows - inputRows - actionBarRows - modalRows - paletteRows - progressRows - taskRows - 2*chatVerticalPad
	if chatHeight < 1 {
		chatHeight = 1
	}

	// Textarea sizes to the chat input box's inner content width — this is
	// what WelcomeRender passes through. Without this, the textarea keeps
	// its initial width and the rendered row doesn't match the resized box.
	inputW := a.width - 2*chatSidePad
	if inputW < 40 {
		inputW = 40
	}
	// Content width inside the box: subtract left border + 2*padding.
	// SyncHeight / soft-wrap and WelcomeRender both read ta.Width(),
	// so we set it here to match what renderInputBox actually renders.
	//
	// CRITICAL: welcome view renders the box at a FIXED width (80, clamped
	// to terminal width) — different from chat-mode's full-width input.
	// If we leave the textarea at chat width, ta.Width() between View()
	// calls disagrees with what was on screen, and bubbles' CursorUp/Down
	// uses the wrong wrap → cursor jumps to the wrong visual row.
	boxW := inputW
	if a.showWelcome {
		boxW = 80
		if a.width < boxW+8 {
			boxW = a.width - 8
		}
		if boxW < 40 {
			boxW = 40
		}
	}
	contentW := boxW - 5
	if contentW < 20 {
		contentW = 20
	}

	if !a.initialized {
		a.chat = view.NewChat(chatW, chatHeight)
		a.chat.SetWelcomeInfo(a.buildWelcomeInfo())
		a.chat.SetForceWelcome(a.showWelcome)
		a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
		a.input = view.NewInput(inputW)
		a.input.SetTextareaWidth(contentW)
		a.input.SetMode(a.cfg.Mode)
		a.initialized = true
	} else {
		a.chat.SetSize(chatW, chatHeight)
		a.input.SetSize(inputW)
		a.input.SetTextareaWidth(contentW)
	}
	// After (re)sizing, recompute soft-wrap visual height so the box
	// grows immediately on resize without waiting for the next keystroke.
	a.input.SyncHeight(5)
	// Width() excludes borders. palette now has 1 left border (▌) matching input box.
	// palette total = SetSize + 1; input total = inputW + 1. After PaddingLeft(1): both = inputW + 2.
	a.slashPalette.SetSize(inputW)
	a.mentionPalette.SetSize(inputW)
	if a.workflowProgress != nil {
		a.workflowProgress.SetSize(inputW)
	}
	if a.taskPanel != nil {
		a.taskPanel.SetSize(inputW)
	}

	// Track input box + task panel positions for mouse.
	if !a.showWelcome {
		taH := a.input.Inner().Height()
		if taH < 1 {
			taH = 1
		}
		meta := 0
		inputBoxHeight := meta + taH + 5 + taskRows
		a.inputBoxTopY = a.height - statusBarRows - inputBoxHeight
		if a.inputBoxTopY < 0 {
			a.inputBoxTopY = 0
		}
		a.taskPanelHeight = taskRows
		if taskRows > 0 {
			a.taskPanelTopY = a.inputBoxTopY
			a.inputRowY = a.inputBoxTopY + taskRows + 1 + meta + 1
		} else {
			a.taskPanelTopY = -1
			a.inputRowY = a.inputBoxTopY + 1 + meta + 1
		}
		a.inputColX = chatSidePad + 1 + 2
		a.chatTopY = chatVerticalPad
		a.statusBarRowY = a.height - 1
	} else {
		// Welcome view: rough estimate
		a.taskPanelTopY = -1
		a.taskPanelHeight = 0
		a.inputRowY = a.height / 2
		a.inputColX = (a.width-80)/2 + 3
		if a.inputColX < 3 {
			a.inputColX = 3
		}
	}
}

// buildDiffFiles converts lastCommitDiff to session DiffFile slice.
func (a *App) buildDiffFiles() []state.DiffFile {
	if len(a.lastCommitDiff) == 0 {
		return nil
	}
	out := make([]state.DiffFile, len(a.lastCommitDiff))
	for i, fd := range a.lastCommitDiff {
		out[i] = state.DiffFile{Path: fd.Path, Before: fd.Before, After: fd.After}
	}
	return out
}

func (a *App) showCommitDiff() {
	if len(a.lastCommitDiff) == 0 && !a.session.HasDiff() {
		return
	}
	// Prefer toggling in-chat RoleDiff when lastCommitDiff is empty (reopen).
	if len(a.lastCommitDiff) == 0 {
		if a.session.ToggleLastDiff() {
			a.diffShown = a.session.HasDiff()
			a.chat.SetMessages(a.session.Messages)
			a.layout()
			a.updateStatusHints()
		}
		return
	}
	if a.diffShown {
		a.session.RemoveDiff()
		a.diffShown = false
	} else {
		a.session.AddDiffFiles(a.buildDiffFiles())
		a.diffShown = true
	}
	a.chat.SetMessages(a.session.Messages)
	a.layout()
	a.updateStatusHints()
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

// syncDiffStateFromSession rebuilds lastCommitDiff from RoleDiff messages so
// /diff and d work after session reopen.
func (a *App) syncDiffStateFromSession() {
	a.lastCommitDiff = nil
	a.diffShown = false
	for _, m := range a.session.Messages {
		if m.Role != state.RoleDiff || len(m.DiffFiles) == 0 {
			continue
		}
		out := make([]rpcclient.FileDiff, len(m.DiffFiles))
		for i, df := range m.DiffFiles {
			out[i] = rpcclient.FileDiff{Path: df.Path, Before: df.Before, After: df.After}
		}
		a.lastCommitDiff = out
		a.diffShown = true
		return
	}
}

// renderInputBox renders the boxed text-input used in both the welcome screen
// and the chat view. Bottom row: build · model · provider · exec (mode lives here only).
func (a *App) renderInputBox(width int) string {
	t := view.ThemeForApp()
	bg := t.BackgroundSecondary()

	contentW := width - 5
	if contentW < 20 {
		contentW = 20
	}

	savedW := a.input.TextareaWidth()
	a.input.SetTextareaWidth(contentW)
	defer a.input.SetTextareaWidth(savedW)

	taLine := a.input.WelcomeRender(contentW, a.cursorBlink)
	gapLine := padLinesBg("", contentW, bg)
	modeLine := padLinesBg(a.welcomeModeLine(), contentW, bg)

	boxContent := lipgloss.JoinVertical(lipgloss.Left, taLine, gapLine, modeLine)

	mode := a.cfg.Mode
	if mode == "" {
		mode = "build"
	}
	return lipgloss.NewStyle().
		Background(bg).
		Border(lipgloss.OuterHalfBlockBorder(), false, false, false, true).
		BorderForeground(view.ModeColor(mode)).
		BorderBackground(bg).
		Padding(1, 2).
		Width(width).
		Render(boxContent)
}

// agentModes lists the available agent modes cycled by Tab. The names must
// match the built-in modes recognized by internal/config — otherwise the
// core returns "unknown agent mode" on agent.run.
var agentModes = []string{"build", "plan", "explore"}

// cycleAgentMode advances cfg.Mode to the next mode in agentModes.
func (a *App) cycleAgentMode() {
	cur := a.cfg.Mode
	if cur == "" {
		cur = agentModes[0]
	}
	idx := -1
	for i, m := range agentModes {
		if m == cur {
			idx = i
			break
		}
	}
	a.cfg.Mode = agentModes[(idx+1)%len(agentModes)]
	a.input.SetMode(a.cfg.Mode)
	a.chat.SetMeta(a.cfg.Mode, a.cfg.Model)
}

// updateStatusHints updates the status bar hint text based on the current UI state.
func (a *App) updateStatusHints() {
	switch {
	case a.mousePassthrough:
		a.statusBar.SetHints("Выделение мышью · Ctrl+G для возврата")
	case a.paletteActive:
		a.statusBar.SetHints("↑↓ выбор · Enter выполнить · Esc")
	case a.mentionActive:
		a.statusBar.SetHints("↑↓ выбор · Enter/Tab вставить · Esc")
	case a.questionModal != nil:
		a.statusBar.SetHints("Enter — ответ · Esc — отмена")
	case a.permModal != nil:
		a.statusBar.SetHints("[y] раз · [a] сессия · [t] tool · [n] нет")
	case a.turn.CanCancel() && a.activeCancel != nil:
		a.statusBar.SetHints("Esc отмена · Ctrl+C")
	case !a.turn.ShowBusySpinner() && a.pendingTodoCount() > 0:
		a.statusBar.SetHints("Ctrl+T · «продолжай»")
	case a.taskPanelOpen && len(a.todos) > 0:
		a.statusBar.SetHints("↑↓ · Ctrl+T")
	case !a.turn.ShowBusySpinner() && a.session.HasDiff():
		a.statusBar.SetHints("d / Ctrl+D · diff")
	case !a.turn.ShowBusySpinner() && a.sessionHasTools():
		a.statusBar.SetHints("Ctrl+T · tools")
	default:
		a.statusBar.SetHints("")
	}
	a.syncStatusBar()
}

func (a *App) sessionHasTools() bool {
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		if a.session.Messages[i].Role == state.RoleAssistant && len(a.session.Messages[i].ToolBlocks) > 0 {
			return true
		}
	}
	return false
}

// renderToast renders a centered floating notification box.
func (a *App) renderToast() string {
	if a.toastText == "" {
		return ""
	}
	t := view.ThemeForApp()
	inner := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(t.BackgroundSecondary()).
		Padding(0, 2).
		Render(a.toastText)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Background(t.BackgroundSecondary()).
		Render(inner)
	return lipgloss.Place(a.width, 3, lipgloss.Center, lipgloss.Top, box)
}

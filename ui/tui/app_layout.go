package tui

import (
	"github.com/orchestra/orchestra/ui/tui/view"
)

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
	// Task panel overlays chat вЂ” does not consume layout rows (avoids input jumping).
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

	// Textarea sizes to the chat input box's inner content width вЂ” this is
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
	// to terminal width) вЂ” different from chat-mode's full-width input.
	// If we leave the textarea at chat width, ta.Width() between View()
	// calls disagrees with what was on screen, and bubbles' CursorUp/Down
	// uses the wrong wrap в†’ cursor jumps to the wrong visual row.
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
	// Width() excludes borders. palette now has 1 left border (в–Њ) matching input box.
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

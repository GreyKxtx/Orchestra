package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/view"
)

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

	// Chat is the only component narrowed by chatSidePad — everything else
	// (input box with its border + bg, action bar, palette, status bar) keeps
	// the full terminal width and uses its own internal padding.
	parts := []string{a.padChat(a.chat.Render())}
	if a.pendingOps != nil {
		parts = append(parts, a.renderActionBar())
	}
	if a.paletteActive && len(a.slashPalette.Items) > 0 {
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(a.slashPalette.Render()))
	}
	if a.mentionActive && len(a.mentionPalette.Items) > 0 {
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(chatSidePad).Render(a.mentionPalette.Render()))
	}
	if a.permModal != nil {
		parts = append(parts, a.permModal.Render())
	} else {
		parts = append(parts, a.renderChatInputBox())
	}
	parts = append(parts, a.statusBar.Render())

	screen := strings.Join(parts, "\n")

	// Command modal overlaid on top.
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

// renderActionBar draws the "⏵ N pending ops · [a]pply · [d]iff · [x]discard"
// banner shown above the input when the agent produced patches awaiting
// user approval. Full terminal width with its own internal padding.
func (a *App) renderActionBar() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e0af68")).
		Width(a.width).
		Padding(0, 1)
	count := len(a.pendingOps.Ops)
	return style.Render(fmt.Sprintf("⏵ %d pending ops · [a]pply · [d]iff · [x]discard", count))
}

// layout recomputes child sizes based on current width/height. Called on
// WindowSize and whenever a layout-affecting state changes (agent busy,
// permission modal, pending ops, palette active).
//
// Only the chat scrollback is narrowed by 2*chatSidePad cells so its content
// has a gutter from the edges. Input box, palette, action bar and status bar
// render at full terminal width and rely on their own internal padding.
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
	if a.pendingOps != nil {
		actionBarRows = 1
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
	// Boxed input: pad-top(1) + textarea(N rows) + gap(1) + modeline(1) +
	// pad-bottom(1) — measured exactly from a sample render so a multi-line
	// textarea (or a future palette-mode swap) doesn't desync the layout.
	// Falls back to the static 5-row total when the input isn't constructed
	// yet (first WindowSize before NewInput).
	// Welcome-style box: 1 pad-top + textarea + gap + modeline + 1 pad-bottom
	// = 5 rows, fixed. Don't pay the full renderChatInputBox cost on every
	// resize / layout call — that one allocates a non-trivial chain of
	// lipgloss styles, which manifests as visible flicker during a sustained
	// terminal-resize sequence.
	inputRows := 5
	modalRows := 0
	if a.permModal != nil {
		inputRows = 0 // modal replaces input
		modalRows = 5
		a.permModal.SetSize(a.width)
	}
	// Status bar now reserves 2 rows (1 gap + 1 content) for breathing room
	// between the input box and the status text.
	const statusBarRows = 2
	chatHeight := a.height - statusBarRows - inputRows - actionBarRows - modalRows - paletteRows - 2*chatVerticalPad
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
	// +1 compensates for the palette's border+padding footprint (4 cells) vs
	// the input box's border+padding footprint (5 cells), keeping their left
	// edges aligned after PaddingLeft(chatSidePad) is applied in View().
	a.slashPalette.SetSize(inputW + 1)
	a.mentionPalette.SetSize(inputW + 1)

	// Track input box position for mouse click-to-cursor.
	if !a.showWelcome {
		// Box layout (no top border, only left border ▌):
		//   top pad (1) + textarea (taH) + gap (1) + modeLine (1) + bottom pad (1) = 4 + taH rows.
		// Box ends at screen row h-2 (status bar at h-1). Box starts at h-1-inputBoxHeight.
		// First textarea row is at boxStart + 1 (skip top pad) = h - inputBoxHeight.
		taH := a.input.Inner().Height()
		if taH < 1 {
			taH = 1
		}
		inputBoxHeight := 4 + taH
		a.inputRowY = a.height - inputBoxHeight // first textarea row inside the box
		a.inputColX = chatSidePad + 1 + 2        // sidePad + border(▌) + leftPad
		// chatTopY: padChat adds PaddingTop(chatVerticalPad) above the viewport,
		// so the first viewport content row is at screen row chatVerticalPad.
		a.chatTopY = chatVerticalPad
	} else {
		// Welcome view: rough estimate
		a.inputRowY = a.height / 2
		a.inputColX = (a.width-80)/2 + 3
		if a.inputColX < 3 {
			a.inputColX = 3
		}
	}
}

// buildDiffContent renders the diff for the current pending-ops set as a
// single text block that fits the chat width.
func (a *App) buildDiffContent() string {
	if a.pendingOps == nil || len(a.pendingOps.Diff) == 0 {
		return "(no diff available)"
	}
	diffs := make([]view.FileDiffView, len(a.pendingOps.Diff))
	for i, fd := range a.pendingOps.Diff {
		diffs[i] = view.FileDiffView{Path: fd.Path, Before: fd.Before, After: fd.After}
	}
	return view.RenderAllDiffs(diffs, a.width)
}

// renderInputBox renders the boxed text-input used in both the welcome screen
// and the chat view. ▌ accent in mode color, panel BG, padding 1×2:
//
//	┌── ▌ ──────────────────────┐
//	│  > textarea               │
//	│                           │   ← gap
//	│  build · model · LMStudio │   ← status row INSIDE the box
//	└───────────────────────────┘
func (a *App) renderInputBox(width int) string {
	t := view.ThemeForApp()
	bg := t.BackgroundSecondary()

	contentW := width - 5 // 1 border + 2 left pad + 2 right pad
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
		a.statusBar.SetHints("↑↓ select · Enter execute · Esc cancel")
	case a.mentionActive:
		a.statusBar.SetHints("↑↓ select · Enter/Tab insert · Esc cancel")
	case a.permModal != nil:
		a.statusBar.SetHints("[y]es allow · [n]o deny · Esc deny")
	case a.pendingOps != nil:
		a.statusBar.SetHints("[a]pply · [d]iff · [x]discard · Ctrl+C quit")
	default:
		a.statusBar.SetHints("Ctrl+G выделение · Ctrl+K команды")
	}
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

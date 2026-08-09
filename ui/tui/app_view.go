package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

const statusBarScreenRows = 2

// chatSidePad is the symmetric horizontal margin applied to the chat
// scrollback area only вЂ” so individual messages don't hug the terminal edge.
// The input box, action bar, palette and status bar render at the full
// terminal width: they have their own internal padding / border / bg, and
// wrapping them in an outer style would re-flow already laid-out cells and
// visually shred multi-line, bordered, bg-styled blocks (the "sliced" input
// bug). Welcome view has its own centered layout вЂ” unaffected.
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

// View renders the full screen layout вЂ” top-of-screen dispatcher that picks
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
		parts = append(parts, a.renderStickyTasks())
		parts = append(parts, a.renderChatInputBox())
	} else if a.permModal != nil {
		parts = append(parts, a.renderStickyTasks())
		parts = append(parts, a.permModal.Render())
	} else {
		parts = append(parts, a.renderStickyTasks())
		parts = append(parts, a.renderChatInputBox())
	}
	parts = append(parts, a.statusBar.Render())
	return parts
}

// renderStickyTasks pins the live checklist above the input (Claude-style),
// so scrolling the transcript does not hide what the agent is working on.
func (a *App) renderStickyTasks() string {
	items := todosToState(a.todos)
	if len(items) == 0 {
		return ""
	}
	w := a.width - 2*chatSidePad
	if w < 40 {
		w = 40
	}
	panel := view.RenderTodosChecklistCapped(items, w, a.turn.ShowBusySpinner(), a.spinFrame, 5)
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

// renderChatInputBox renders the same grey-bg в–Њ box that the welcome screen
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

// renderInputBox renders the boxed text-input used in both the welcome screen
// and the chat view. Bottom row: build · model · provider · exec (mode lives here only).
func (a *App) renderInputBox(width int) string {
	t := view.ThemeForApp()
	bg := t.BackgroundSecondary()

	// Width includes horizontal padding (1,2) but not the left border.
	// contentW = width - 4 (pad) - 1 (slack / │ caret) — matches pre-compact math.
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

	// Textarea, spacer, then mode/model row — spacer keeps "Спроси…" from
	// sitting flush against build · model · …
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
// match the built-in modes recognized by internal/config вЂ” otherwise the
// core returns "unknown agent mode" on agent.run.
var agentModes = []string{"build", "plan", "explore", "ask", "debug", "architecture", "agent", "orchestra"}

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
	a.routeBadge = ""
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
		if n := a.queuedMessageCount(); n > 0 {
			a.statusBar.SetHints(fmt.Sprintf("Esc отмена · в очереди: %d", n))
		} else {
			a.statusBar.SetHints("Esc отмена · Ctrl+C")
		}
	case !a.turn.ShowBusySpinner() && a.pendingTodoCount() > 0:
		a.statusBar.SetHints("«продолжай» · Shift+Tab shell")
	case !a.turn.ShowBusySpinner() && a.actionBarActive():
		if a.session.LastDiffExpanded() {
			a.statusBar.SetHints("↑↓ · a/x file · Enter apply · d collapse · shift+x discard")
		} else {
			a.statusBar.SetHints("[a] apply · [d] diff · [x] discard")
		}
	case !a.turn.ShowBusySpinner() && a.session.LastDiffExpanded():
		if a.pendingReview {
			a.statusBar.SetHints("↑↓ · a принять · x откат · Enter apply")
		} else {
			a.statusBar.SetHints("↑↓ · a принять · x откат · Enter все")
		}
	case !a.turn.ShowBusySpinner() && a.session.HasDiff():
		a.statusBar.SetHints("d / Ctrl+D · diff · Shift+Tab shell")
	case !a.turn.ShowBusySpinner() && a.sessionHasTools():
		a.statusBar.SetHints("Ctrl+T · tools · Shift+Tab shell")
	default:
		if n := len(a.stagedAttachments); n > 0 {
			a.statusBar.SetHints(fmt.Sprintf("📎 %d влож. · Enter отправить · /attach ещё", n))
		} else {
			a.statusBar.SetHints("Tab · mode · Shift+Tab · shell · /attach")
		}
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

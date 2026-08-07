package tui

import (
	"strings"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// inputEmpty reports whether the composer has no text (hotkeys may steal keys).
func (a *App) inputEmpty() bool {
	return strings.TrimSpace(a.input.Value()) == ""
}

// tryChromeHotkey handles Ctrl+T / Ctrl+R / bare t|d / Ctrl+D-as-diff.
// Returns true when the key was consumed (do not type or scroll).
// Permission-modal "t" must be handled by the caller before this.
func (a *App) tryChromeHotkey(key string) bool {
	switch key {
	case "ctrl+t":
		a.handleCtrlTCascade()
		return true
	case "ctrl+r":
		return a.toggleLastReasoning()
	case "t":
		if !a.inputEmpty() {
			return false
		}
		if len(a.todos) > 0 {
			a.toggleTaskPanel()
			return true
		}
		return a.handleCtrlTCascade()
	case "d":
		if !a.inputEmpty() || !a.hasCommitDiff() {
			return false
		}
		a.showCommitDiff()
		return true
	case "ctrl+d":
		// Diff toggle when empty+diff available; otherwise caller scrolls.
		if !a.inputEmpty() || !a.hasCommitDiff() {
			return false
		}
		a.showCommitDiff()
		return true
	default:
		return false
	}
}

func (a *App) hasCommitDiff() bool {
	return len(a.lastCommitDiff) > 0 || a.session.HasDiff()
}

// handleCtrlTCascade: close open Tasks; else open Tasks when todos exist;
// else expand tools; else toggle diff. Tasks strip advertises Ctrl+T.
func (a *App) handleCtrlTCascade() bool {
	if a.taskPanelOpen {
		a.toggleTaskPanel()
		return true
	}
	if len(a.todos) > 0 {
		a.toggleTaskPanel()
		return true
	}
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		m := a.session.Messages[i]
		if m.Role == state.RoleAssistant && len(m.ToolBlocks) > 0 {
			a.toggleToolsExpanded(i)
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
	return false
}

// toggleToolsExpanded flips Message.ToolsExpanded (persist SoT) and mirrors
// the chat expand cache so render invalidation stays consistent.
func (a *App) toggleToolsExpanded(i int) {
	m := &a.session.Messages[i]
	m.ToolsExpanded = !m.ToolsExpanded
	a.chat.SetTurnExpanded(view.MessageKey(*m), m.ToolsExpanded)
	a.chat.SetMessages(a.session.Messages)
	a.layout()
	a.updateStatusHints()
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

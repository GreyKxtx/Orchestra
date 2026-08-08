package tui

import (
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// handleMouseMsg processes wheel, click, drag, and right-click copy for chat/input.
func (a *App) handleMouseMsg(m tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.Button == tea.MouseButtonWheelUp && m.Action == tea.MouseActionPress:
			a.chat.ScrollUp(3)
			return a, nil
		case m.Button == tea.MouseButtonWheelDown && m.Action == tea.MouseActionPress:
			a.chat.ScrollDown(3)
			return a, nil
		case m.Button == tea.MouseButtonLeft && m.Action == tea.MouseActionPress:
			if !a.showWelcome && m.Y == a.statusBarRowY {
				_ = m.X
			}
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
				if m.Y >= topY && m.Y < a.inputRowY && !a.turn.BlocksInput() && !a.showWelcome {
					contentY := a.chat.ViewportYOffset() + (m.Y - topY)
					role, text, ok := a.chat.MessageAtContentY(contentY)
					if ok && text != "" {
						// Same dialog for user and assistant (Copy; Edit only for user).
						a.pushDialog(view.NewMessageActionDialog(text, role == state.RoleUser))
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

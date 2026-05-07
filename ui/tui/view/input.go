package view

import (
	"github.com/charmbracelet/bubbles/textarea"
)

// Input is the multiline editor at the bottom of the screen.
type Input struct {
	ta textarea.Model
}

// NewInput creates a sized textarea.
func NewInput(width int) Input {
	ta := textarea.New()
	ta.Placeholder = "Спроси Orchestra…"
	ta.SetWidth(width)
	ta.SetHeight(3) // grows up to ~6 visually via Bubble Tea wrapping
	ta.ShowLineNumbers = false
	ta.Focus()
	return Input{ta: ta}
}

// SetSize resizes the textarea width.
func (in *Input) SetSize(width int) {
	in.ta.SetWidth(width)
}

// Value returns the current input text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// Render returns the textarea view.
func (in Input) Render() string { return in.ta.View() }

// Inner returns the underlying textarea model so app.go can route Bubble Tea
// messages to it (key events, etc.). Phase 1 only.
func (in *Input) Inner() *textarea.Model { return &in.ta }

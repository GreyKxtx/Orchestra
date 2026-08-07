package view

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the bottom editor — direct port of OpenCode's editor.View().
//
//	style.Render(">") + textarea.View()
type Input struct {
	ta        textarea.Model
	width     int
	taWidth   int    // outer width last passed to SetTextareaWidth (≠ in.ta.Width())
	mode      string // current agent mode; drives prompt color
	selAnchor int    // -1 = no selection; ≥0 = rune index where selection started
	// Mouse drag caret: when mouseCaretActive, renders at mouseCaret instead of ta cursor.
	mouseCaret       int
	mouseCaretActive bool
}

// NewInput creates a textarea styled like OpenCode's CreateTextArea.
// The textarea uses BackgroundSecondary so the grey input box stands out
// against the main BG (#0d0d0d vs #1a1a1a) — same visual as OpenCode.
func NewInput(width int) Input {
	t := theme.CurrentTheme()
	bgColor := t.BackgroundSecondary()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	base := lipgloss.NewStyle()

	ta := textarea.New()
	bgFill := base.Background(bgColor)
	ta.BlurredStyle.Base = bgFill.Foreground(textColor)
	ta.BlurredStyle.CursorLine = bgFill
	ta.BlurredStyle.CursorLineNumber = bgFill
	ta.BlurredStyle.LineNumber = bgFill
	ta.BlurredStyle.Placeholder = bgFill.Foreground(textMutedColor)
	ta.BlurredStyle.Prompt = bgFill
	ta.BlurredStyle.Text = bgFill.Foreground(textColor)
	ta.BlurredStyle.EndOfBuffer = bgFill

	ta.FocusedStyle.Base = bgFill.Foreground(textColor)
	ta.FocusedStyle.CursorLine = bgFill
	ta.FocusedStyle.CursorLineNumber = bgFill
	ta.FocusedStyle.LineNumber = bgFill
	ta.FocusedStyle.Placeholder = bgFill.Foreground(textMutedColor)
	ta.FocusedStyle.Prompt = bgFill
	ta.FocusedStyle.Text = bgFill.Foreground(textColor)
	ta.FocusedStyle.EndOfBuffer = bgFill

	// Cursor — without explicit styling it renders as terminal-default reverse
	// video (black bg) which leaks through our grey input box. Set both the
	// "on" Style and the "under cursor" TextStyle to use our bg.
	ta.Cursor.Style = lipgloss.NewStyle().Background(t.Primary()).Foreground(bgColor)
	ta.Cursor.TextStyle = lipgloss.NewStyle().Background(bgColor).Foreground(textColor)

	ta.Placeholder = "Спроси Orchestra…"
	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	// Unlimited logical lines — default MaxHeight=99 silently drops newlines
	// after the 99th line during paste / InsertNewline.
	ta.MaxHeight = 0
	ta.SetWidth(width - 2) // " >" prompt occupies 2 cols
	ta.SetHeight(1)

	// Extend word-jump bindings so Ctrl+Left/Right also trigger word
	// navigation (bubbles defaults only bind Alt+Left/B and Alt+Right/F,
	// which doesn't match the desktop-editor convention users expect).
	ta.KeyMap.WordBackward = key.NewBinding(key.WithKeys("alt+left", "alt+b", "ctrl+left"))
	ta.KeyMap.WordForward = key.NewBinding(key.WithKeys("alt+right", "alt+f", "ctrl+right"))

	ta.Focus()
	return Input{ta: ta, width: width, taWidth: width - 2, selAnchor: -1}
}

// SetMode stores the agent mode so the prompt prefix can be colored to match
// the mode label elsewhere in the UI.
func (in *Input) SetMode(mode string) { in.mode = mode }

// SetSize resizes the textarea.
func (in *Input) SetSize(width int) {
	in.width = width
	in.taWidth = width - 2
	in.ta.SetWidth(width - 2)
}

// SetTextareaWidth lets external renderers (e.g. welcome view) set the
// textarea width without touching the input's own width tracking.
// We remember the EXACT width we were asked for so TextareaWidth can
// return it later — bubbles' internal m.width is post-decremented by
// the prompt width, which would otherwise cause save/restore dances
// (renderInputBox does one) to drift the width by 1 cell per render.
func (in *Input) SetTextareaWidth(w int) {
	in.taWidth = w
	in.ta.SetWidth(w)
}

// TextareaWidth returns the width that was last passed to SetTextareaWidth
// (i.e. the OUTER width including prompt), suitable for restoration.
func (in Input) TextareaWidth() int {
	if in.taWidth > 0 {
		return in.taWidth
	}
	return in.ta.Width()
}

// Value returns the current text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// SetValue replaces the current text.
func (in *Input) SetValue(s string) { in.ta.SetValue(s) }

// Inner returns the underlying textarea so app.go can route key events.
func (in *Input) Inner() *textarea.Model { return &in.ta }

// CursorPos returns the absolute rune index of the textarea cursor across all lines.
func (in Input) CursorPos() int { return absolutePos(in.ta) }

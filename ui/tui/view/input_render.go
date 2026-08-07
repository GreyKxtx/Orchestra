package view

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// the current agent-mode color so the prompt, the welcome input bar, and
// the mode label all share one accent across views.
func (in Input) Render() string {
	style := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(ModeColor(in.mode))
	return lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"), in.ta.View())
}

// WelcomeRender renders the input rows for the welcome view and chat box
// ourselves, bypassing bubbles textarea.View(). Renders each logical line
// (split by '\n') on its own row with consistent overlay (cursor + selection).
//
//	width    — target visible width of each row (matches box content area)
//	blinkOn  — whether the cursor is currently visible (animation)
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle := lipgloss.NewStyle().Background(bg)
	textStyle := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	mentionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4fc3f7")).
		Background(lipgloss.Color("#163044")).
		Bold(true).
		Underline(true)
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	selStyle := lipgloss.NewStyle().Background(t.BorderNormal()).Foreground(t.Text())
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	totalRunes := len([]rune(val))
	mentions := mentionSpans([]rune(val))

	// Empty input — placeholder is always visible; only the bar cursor in
	// front of it blinks (replaced with a same-width space when off) so the
	// placeholder text itself doesn't flicker on/off with each frame.
	if val == "" {
		ph := mutedStyle.Render("Спроси Orchestra…")
		if blinkOn {
			return padLine(bar+ph, width, bgStyle)
		}
		return padLine(bgStyle.Render(" ")+ph, width, bgStyle)
	}

	// Resolve cursor absolute position (mouse-caret override during drag).
	var cursorPos int
	if in.mouseCaretActive {
		cursorPos = clampPos(in.mouseCaret, totalRunes)
	} else {
		cursorPos = clampPos(absolutePos(in.ta), totalRunes)
	}
	selMin, selMax, hasSel := in.SelectionRange()

	// Build visual chunks using the SAME word-aware wrap as bubbles' internal
	// textarea (see wordWrap below). Char-at-wrapW chunking disagrees with
	// bubbles whenever the text contains spaces — that mismatch shifts the
	// rendered cursor away from where bubbles' CursorUp/CursorDown actually
	// moved it. `width` is still used by padLine to pad each rendered row to
	// the outer box width.
	wrapW := in.WrapWidth()
	if wrapW < 1 {
		wrapW = width
	}
	if wrapW < 1 {
		wrapW = 1
	}
	chunks := in.VisualRows(wrapW)

	rendered := make([]string, 0, len(chunks))
	for _, c := range chunks {
		var b strings.Builder
		b.Grow(len(c.Runes) * 20)
		for i, r := range c.Runes {
			absIdx := c.AbsStart + i
			ch := string(r)
			isCursor := blinkOn && absIdx == cursorPos
			isSelected := hasSel && absIdx >= selMin && absIdx < selMax
			isMention := inMention(mentions, absIdx)
			switch {
			case isCursor:
				b.WriteString(cursorStyle.Render(ch))
			case isSelected:
				b.WriteString(selStyle.Render(ch))
			case isMention:
				b.WriteString(mentionStyle.Render(ch))
			default:
				b.WriteString(textStyle.Render(ch))
			}
		}
		// Bar cursor at end of THIS chunk iff cursor sits there AND
		// this chunk is the end of a logical line — otherwise the
		// position equals the start of the next chunk (continuation
		// wrap), where the block cursor will be drawn instead.
		endOfChunkAbs := c.AbsStart + len(c.Runes)
		if blinkOn && cursorPos == endOfChunkAbs && c.EndOfLogical {
			b.WriteString(bar)
		}
		rendered = append(rendered, padLine(b.String(), width, bgStyle))
	}

	return strings.Join(rendered, "\n")
}

// padLine pads s to exactly width visible cells using bgStyle-filled spaces.
func padLine(s string, width int, bgStyle lipgloss.Style) string {
	if diff := width - lipgloss.Width(s); diff > 0 {
		s += bgStyle.Render(strings.Repeat(" ", diff))
	}
	return s
}

// absolutePos computes the absolute rune index of the textarea cursor,
// accounting for both logical lines (separated by '\n') and soft-wrap.
//
// StartColumn is the rune index within the current logical line where
// the current wrapped visual row starts; ColumnOffset is the rune offset

func mentionSpans(runes []rune) [][2]int {
	var spans [][2]int
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if i > 0 && !unicode.IsSpace(runes[i-1]) && runes[i-1] != '\n' {
			continue
		}
		j := i + 1
		for j < len(runes) && !unicode.IsSpace(runes[j]) && runes[j] != '\n' {
			j++
		}
		if j > i+1 {
			spans = append(spans, [2]int{i, j})
		}
		i = j - 1
	}
	return spans
}

func inMention(spans [][2]int, idx int) bool {
	for _, sp := range spans {
		if idx >= sp[0] && idx < sp[1] {
			return true
		}
	}
	return false
}

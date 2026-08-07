package view

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// renderAssistantMessage — chronological segments (Claude/OpenCode parts model).
//
//	for each segment in order: reasoning | tool group | text | notice
//	thinking placeholder while streaming and nothing has landed yet
//	footer ▣ Mode · Model · Xs · Y tokens — finished turns
//
// userQuery: the preceding RoleUser message text (passes into the tool-group title).
func (c Chat) renderAssistantMessage(m state.Message, width int, isLast bool, userQuery string) string {
	var parts []string

	segs := m.Segments
	if len(segs) == 0 {
		// Legacy flat message (should be rare after NormalizeSegments).
		if strings.TrimSpace(m.Reasoning) != "" {
			segs = append(segs, state.Segment{Kind: state.SegmentReasoning, Text: m.Reasoning})
		}
		if len(m.ToolBlocks) > 0 {
			segs = append(segs, state.Segment{Kind: state.SegmentTools, Tools: m.ToolBlocks})
		}
		if strings.TrimSpace(m.Text) != "" {
			segs = append(segs, state.Segment{Kind: state.SegmentText, Text: m.Text})
		}
		for _, n := range m.Notices {
			segs = append(segs, state.Segment{Kind: state.SegmentNotice, Text: n.Text, NoticeKind: n.Kind})
		}
	}

	msgMode := m.Mode
	if msgMode == "" {
		msgMode = c.chatMode
	}
	toolsExpanded := m.Streaming || m.ToolsExpanded
	lastTextIdx := -1
	for i := range segs {
		if segs[i].Kind == state.SegmentText && strings.TrimSpace(segs[i].Text) != "" {
			lastTextIdx = i
		}
	}

	for i, seg := range segs {
		switch seg.Kind {
		case state.SegmentReasoning:
			if strings.TrimSpace(seg.Text) == "" {
				continue
			}
			streamingTail := m.Streaming && lastTextIdx < 0 && i == len(segs)-1
			parts = append(parts, renderReasoning(seg.Text, width, streamingTail, c.spinFrame, m.ReasoningExpanded))
		case state.SegmentTools:
			if len(seg.Tools) == 0 {
				continue
			}
			parts = append(parts, c.renderToolGroup(seg.Tools, width, toolsExpanded, m.Streaming, 0, userQuery, msgMode))
		case state.SegmentText:
			text := stripFinalEnvelope(seg.Text)
			if strings.TrimSpace(text) == "" {
				continue
			}
			body := renderMarkdown(text, width-2)
			if isLast && m.Streaming && i == lastTextIdx && c.streamCursor {
				body += lipgloss.NewStyle().Foreground(theme.CurrentTheme().Primary()).Render("▋")
			}
			parts = append(parts, lipgloss.NewStyle().PaddingLeft(2).Render(body))
		case state.SegmentNotice:
			if line := RenderAssistantNotice(seg.NoticeKind, seg.Text, width); line != "" {
				parts = append(parts, line)
			}
		}
	}

	if len(parts) == 0 && !m.Streaming {
		// Reasoning models sometimes finish with blank content and no tool_calls.
		t := theme.CurrentTheme()
		hint := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true).
			Render("Model finished with no text. Check tool calling in LM Studio or set enable_thinking: false.")
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(2).Render(hint))
	}

	// Inline "Thinking…" placeholder — opencode style.
	if isLast && m.Streaming && !m.HasVisibleContent() {
		parts = append(parts, renderInlineThinking(c.spinFrame))
	}

	mode := m.Mode
	if mode == "" {
		mode = c.chatMode
	}
	model := m.Model
	if model == "" {
		model = c.chatModel
	}
	// Live footer while streaming; final footer when done (includes tokens).
	if m.Streaming {
		if dur := turnElapsed(m); dur > 0 || !m.StartedAt.IsZero() {
			parts = append(parts, assistantFooter(mode, model, dur, 0, 0))
		}
	} else {
		parts = append(parts, assistantFooter(mode, model, m.Duration, m.TokensIn, m.TokensOut))
	}

	return strings.Join(parts, "\n\n")
}

// renderInlineThinking — single muted "⠋ Thinking…" placeholder shown inside
// an in-flight assistant message until something user-visible streams in.
// Indented col 2 to align with the message body.
func renderInlineThinking(spinFrame int) string {
	s := CurrentStyles()
	spin := s.Primary.Render(SpinnerFrames[spinFrame%len(SpinnerFrames)])
	label := s.Muted.Italic(true).Render("Thinking…")
	return lipgloss.NewStyle().PaddingLeft(2).Render(spin + " " + label)
}

// reasoningCollapsedMaxLines is the default CoT preview height; Ctrl+R expands.
const reasoningCollapsedMaxLines = 6

// renderReasoning renders the model's chain-of-thought with a muted ┃ left bar
// matching the user-message panel style but without a fill — italic warning-
// colored "Thinking:" lead-in followed by italic muted body text.
// Long CoT is collapsed unless expanded or still streaming.
func renderReasoning(text string, width int, stillThinking bool, spinFrame int, expanded bool) string {
	t := theme.CurrentTheme()

	prefix := lipgloss.NewStyle().Foreground(t.Warning()).Italic(true).Render("Thinking:")
	if stillThinking {
		spin := lipgloss.NewStyle().Foreground(t.Primary()).Render(SpinnerFrames[spinFrame%len(SpinnerFrames)])
		prefix = spin + " " + prefix
	}

	body := strings.TrimSpace(text)
	collapsed := false
	if !expanded && !stillThinking {
		lines := strings.Split(body, "\n")
		if len(lines) > reasoningCollapsedMaxLines {
			body = strings.Join(lines[:reasoningCollapsedMaxLines], "\n")
			collapsed = true
		}
	}
	content := prefix + " " + body
	if collapsed {
		hint := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true).Render("… Ctrl+R развернуть")
		content += "\n" + hint
	} else if expanded && !stillThinking {
		hint := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true).Render("Ctrl+R свернуть")
		content += "\n" + hint
	}

	const (
		padH   = 2
		indent = 2
		barCol = 1
	)
	inner := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Italic(true).
		Width(width - indent - barCol - 2*padH).
		Render(content)

	box := lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.TextMuted()).
		Padding(0, padH).
		Render(inner)

	return lipgloss.NewStyle().PaddingLeft(indent).Render(box)
}

// assistantFooter — `▣ <Mode> · <model> · <duration> · <tokens>`. Indented
// col 2 to align with the assistant body above it. Empty fields omitted.
func assistantFooter(mode, model string, dur time.Duration, tokensIn, tokensOut int) string {
	t := theme.CurrentTheme()

	if mode == "" {
		mode = "build"
	}

	icon := lipgloss.NewStyle().Foreground(ModeColor(mode)).Render("▣")
	label := lipgloss.NewStyle().Foreground(t.Text()).Render(titlecase(mode))
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	out := icon + " " + label
	if model != "" {
		out += muted.Render(" · " + model)
	}
	if dur > 0 {
		out += muted.Render(" · " + formatDuration(dur))
	}
	if tokensIn+tokensOut > 0 {
		out += muted.Render(" · " + formatTokens(tokensIn+tokensOut))
	}
	return lipgloss.NewStyle().PaddingLeft(2).Render(out)
}

// turnElapsed reports the wall-clock duration of an assistant turn,
// falling back to "now since StartedAt" while still streaming.
func turnElapsed(m state.Message) time.Duration {
	if m.Duration > 0 {
		return m.Duration
	}
	if !m.StartedAt.IsZero() {
		return time.Since(m.StartedAt)
	}
	return 0
}

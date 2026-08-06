package view

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// renderAssistantMessage — port of OpenCode AssistantMessage.
//
//	reasoning (CoT)        — italic muted, no border, indented col 2
//	tool group             — multi-line `│ Task — query` / `└ N toolcalls · Xs`
//	final answer (markdown)— indented col 2
//	thinking placeholder   — `⠋ Thinking…` while streaming and nothing has
//	                         landed yet (no reasoning, no tools, no text)
//	footer ▣ Mode · Model · Xs · Y tokens — last assistant only
//
// key is the stable message key (StartedAt.UnixNano) used for expand state.
// userQuery: the preceding RoleUser message text (passes into the tool-group title).
func (c Chat) renderAssistantMessage(m state.Message, key int64, width int, isLast bool, userQuery string) string {
	var parts []string

	if strings.TrimSpace(m.Reasoning) != "" {
		parts = append(parts, renderReasoning(m.Reasoning, width, m.Streaming && strings.TrimSpace(m.Text) == "", c.spinFrame, m.ReasoningExpanded))
	}

	if len(m.ToolBlocks) > 0 {
		expanded := m.Streaming || c.expandedTurns[key] || m.ToolsExpanded
		// Use this message's historical Mode for the tool-group title prefix
		// ("Build Task — ...", "Plan Task — ..."), falling back to chat's
		// current mode for legacy sessions.
		msgMode := m.Mode
		if msgMode == "" {
			msgMode = c.chatMode
		}
		parts = append(parts, c.renderToolGroup(m.ToolBlocks, width, expanded, m.Streaming, turnElapsed(m), userQuery, msgMode))
	}

	text := stripFinalEnvelope(m.Text)
	if strings.TrimSpace(text) != "" {
		body := renderMarkdown(text, width-2)
		if isLast && m.Streaming && c.streamCursor {
			body += lipgloss.NewStyle().Foreground(theme.CurrentTheme().Primary()).Render("▋")
		}
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(2).Render(body))
	} else if !m.Streaming &&
		strings.TrimSpace(m.Reasoning) == "" &&
		len(m.ToolBlocks) == 0 {
		// Reasoning models (qwen3.6 via LM Studio) sometimes finish with blank
		// content and no tool_calls — the turn ends but the bubble looks broken.
		t := theme.CurrentTheme()
		hint := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true).
			Render("Модель завершила шаг без текста. Проверьте tool calling в LM Studio или отключите thinking (enable_thinking: false).")
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(2).Render(hint))
	}

	// Inline "Thinking…" placeholder — opencode style. Shown only when the
	// assistant turn is still streaming but nothing user-visible has landed
	// yet (no reasoning prefix, no tool calls, no text). All other states
	// already display their own spinner: reasoning has "⠋ Thinking:", a
	// running tool-group has its own spinner in the title icon.
	if isLast && m.Streaming &&
		strings.TrimSpace(m.Reasoning) == "" &&
		len(m.ToolBlocks) == 0 &&
		strings.TrimSpace(text) == "" {
		parts = append(parts, renderInlineThinking(c.spinFrame))
	}

	if noticeBlock := RenderAssistantNotices(m.Notices, width); noticeBlock != "" {
		parts = append(parts, noticeBlock)
	}

	// Footer is shown on EVERY finished assistant turn — not just the last.
	// The mode/model/duration belong to that specific exchange so the user
	// can scroll back and see what produced each answer. m.Mode/Model are
	// the historical values stored at StartAssistant time; fall back to the
	// current chat config for legacy sessions.
	if !m.Streaming {
		mode := m.Mode
		if mode == "" {
			mode = c.chatMode
		}
		model := m.Model
		if model == "" {
			model = c.chatModel
		}
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

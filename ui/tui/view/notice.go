package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// LocalizeRetryHint maps common English agent hints to Russian for the TUI.
func LocalizeRetryHint(msg string) string {
	msg = strings.TrimSpace(msg)
	msg = strings.TrimPrefix(msg, "[retry]")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	switch {
	case strings.Contains(msg, "Model returned an empty response"):
		return "Пустой ответ модели — вызовите tool (read, grep, ls) или ответьте текстом"
	case strings.Contains(msg, `Do not emit {"patches":[]}`):
		return "Не отправляйте {\"patches\":[]} без tool calls — сначала read, затем edit/write"
	case strings.Contains(msg, `You sent {"patches":[]}`):
		return "Отправлен {\"patches\":[]} без edit/write — прочитайте файл и примените правки"
	case strings.Contains(msg, "reasoning alone is not enough"):
		return "Нужны правки кода — reasoning недостаточно, вызовите read → edit/write"
	case strings.Contains(msg, "no edit/write was performed"):
		return "Правки не выполнены — вызовите read, затем edit или write"
	case strings.Contains(msg, "staged-apply error"), strings.Contains(msg, "StaleContent"):
		return "Контент устарел — перечитайте файл и повторите edit"
	case strings.Contains(msg, "AmbiguousMatch"):
		return "Неоднозначное совпадение — уточните edit или добавьте контекст"
	case strings.Contains(msg, "open todo"):
		return "Есть незакрытые задачи в todolist — не final; отметь done и продолжай следующий пункт"
	case strings.Contains(msg, "max_steps exceeded"), strings.HasPrefix(msg, "MAX_STEPS"):
		return "Лимит шагов хода — история сохранена, напишите продолжить"
	default:
		return msg
	}
}

// RenderAssistantNotice renders one inline info/retry/error line (chronological segment).
func RenderAssistantNotice(kind state.SystemKind, text string, width int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	warn := lipgloss.NewStyle().Foreground(t.Warning())
	icon := "↻"
	label := warn.Render("Retry")
	switch kind {
	case state.SystemKindError:
		icon = "✗"
		label = lipgloss.NewStyle().Foreground(t.Error()).Render("Error")
	case state.SystemKindSuccess:
		icon = "✓"
		label = lipgloss.NewStyle().Foreground(t.Success()).Render("Done")
	case state.SystemKindInfo:
		icon = "●"
		label = muted.Render("Info")
	}
	return renderNoticeLines(muted.Render(icon+" ")+label, muted, text, width)
}

// RenderAssistantNotices renders a stack of notices (legacy / tests). Prefer
// chronological SegmentNotice rendering via RenderAssistantNotice.
func RenderAssistantNotices(notices []state.SystemNotice, width int) string {
	if len(notices) == 0 {
		return ""
	}
	var lines []string
	for _, n := range notices {
		if line := RenderAssistantNotice(n.Kind, n.Text, width); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// RenderSystemMessage renders standalone system lines (committed, workflow, errors).
func RenderSystemMessage(m state.Message, width int) string {
	t := theme.CurrentTheme()
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return ""
	}

	kind := m.SystemKind
	if kind == "" {
		kind = inferSystemKind(text)
	}
	text = stripSystemPrefix(text)

	var icon, label string
	var labelStyle lipgloss.Style
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	switch kind {
	case state.SystemKindError:
		icon = "✗"
		label = "Error"
		labelStyle = lipgloss.NewStyle().Foreground(t.Error())
	case state.SystemKindSuccess:
		icon = "✓"
		label = "Done"
		labelStyle = lipgloss.NewStyle().Foreground(t.Success())
	case state.SystemKindRetry:
		icon = "↻"
		label = "Retry"
		labelStyle = lipgloss.NewStyle().Foreground(t.Warning())
		text = LocalizeRetryHint(text)
	case state.SystemKindInfo:
		icon = "●"
		label = "Info"
		labelStyle = muted
	default:
		icon = "●"
		label = "System"
		labelStyle = muted
	}

	head := muted.Render(icon+" ") + labelStyle.Render(label)
	return renderNoticeLines(head, muted, text, width)
}

// renderNoticeLines paints "icon Label · body" with word-wrapped body so long
// API errors remain readable (no single-line clipPlain truncation).
func renderNoticeLines(head string, muted lipgloss.Style, text string, width int) string {
	const pad = 2
	inner := width - pad
	if inner < 24 {
		inner = 24
	}
	sep := " · "
	headW := lipgloss.Width(head) + lipgloss.Width(sep)
	firstBudget := inner - headW
	if firstBudget < 16 {
		wrapped := wrapPlain(text, inner)
		var out []string
		out = append(out, lipgloss.NewStyle().PaddingLeft(pad).Render(head))
		for _, line := range strings.Split(wrapped, "\n") {
			out = append(out, lipgloss.NewStyle().PaddingLeft(pad).Render(muted.Render(line)))
		}
		return strings.Join(out, "\n")
	}

	wrapped := wrapPlain(text, firstBudget)
	parts := strings.Split(wrapped, "\n")
	first := lipgloss.NewStyle().PaddingLeft(pad).Render(head + muted.Render(sep+parts[0]))
	if len(parts) == 1 {
		return first
	}
	indent := strings.Repeat(" ", headW)
	var out []string
	out = append(out, first)
	for _, line := range parts[1:] {
		out = append(out, lipgloss.NewStyle().PaddingLeft(pad).Render(muted.Render(indent+line)))
	}
	return strings.Join(out, "\n")
}

// wrapPlain wraps plain text to maxCells visible columns (rune-aware).
// Soft-wraps on spaces when possible; hard-breaks oversized tokens.
func wrapPlain(s string, maxCells int) string {
	s = strings.TrimSpace(s)
	if s == "" || maxCells <= 0 {
		return s
	}
	var blocks []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		blocks = append(blocks, wrapPlainParagraph(para, maxCells))
	}
	return strings.Join(blocks, "\n")
}

func wrapPlainParagraph(s string, maxCells int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	var cur string
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, w := range words {
		if lipgloss.Width(w) > maxCells {
			flush()
			r := []rune(w)
			for len(r) > 0 {
				chunk := r
				for len(chunk) > 1 && lipgloss.Width(string(chunk)) > maxCells {
					chunk = chunk[:len(chunk)-1]
				}
				lines = append(lines, string(chunk))
				r = r[len(chunk):]
			}
			continue
		}
		if cur == "" {
			cur = w
			continue
		}
		trial := cur + " " + w
		if lipgloss.Width(trial) <= maxCells {
			cur = trial
			continue
		}
		flush()
		cur = w
	}
	flush()
	return strings.Join(lines, "\n")
}

func inferSystemKind(text string) state.SystemKind {
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "[error]"):
		return state.SystemKindError
	case strings.HasPrefix(lower, "[retry]"):
		return state.SystemKindRetry
	case strings.HasPrefix(lower, "[committed]"):
		return state.SystemKindSuccess
	default:
		return state.SystemKindInfo
	}
}

func stripSystemPrefix(text string) string {
	for _, p := range []string{"[error]", "[retry]", "[committed]"} {
		if strings.HasPrefix(text, p) {
			return strings.TrimSpace(strings.TrimPrefix(text, p))
		}
	}
	return text
}

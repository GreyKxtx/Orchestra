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
	default:
		return msg
	}
}

// RenderAssistantNotices renders inline retry/status lines inside an assistant turn.
func RenderAssistantNotices(notices []state.SystemNotice, width int) string {
	if len(notices) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	warn := lipgloss.NewStyle().Foreground(t.Warning())
	var lines []string
	for _, n := range notices {
		text := strings.TrimSpace(n.Text)
		if text == "" {
			continue
		}
		icon := "↻"
		label := warn.Render("Повтор")
		switch n.Kind {
		case state.SystemKindError:
			icon = "✗"
			label = lipgloss.NewStyle().Foreground(t.Error()).Render("Ошибка")
		case state.SystemKindSuccess:
			icon = "✓"
			label = lipgloss.NewStyle().Foreground(t.Success()).Render("Готово")
		}
		line := muted.Render(icon+" ") + label + muted.Render(" · "+clipPlain(text, width-16))
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(line))
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
		label = "Ошибка"
		labelStyle = lipgloss.NewStyle().Foreground(t.Error())
	case state.SystemKindSuccess:
		icon = "✓"
		label = "Коммит"
		labelStyle = lipgloss.NewStyle().Foreground(t.Success())
	case state.SystemKindRetry:
		icon = "↻"
		label = "Повтор"
		labelStyle = lipgloss.NewStyle().Foreground(t.Warning())
		text = LocalizeRetryHint(text)
	default:
		icon = "●"
		label = "Система"
		labelStyle = muted
	}

	body := labelStyle.Render(label) + muted.Render(" · "+clipPlain(text, width-14))
	line := muted.Render(icon+" ") + body
	return lipgloss.NewStyle().PaddingLeft(2).Render(line)
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

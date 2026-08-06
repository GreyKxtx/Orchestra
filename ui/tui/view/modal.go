package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Modal displays a blocking yes/no dialog for shell command consent.
type Modal struct {
	Tool        string
	Description string
	width       int
}

// NewModal creates a permission modal for the given tool request.
func NewModal(tool, description string) *Modal {
	return &Modal{Tool: tool, Description: description}
}

// SetSize updates the modal's known width.
func (m *Modal) SetSize(width int) { m.width = width }

// QuestionModal displays a blocking dialog for agent question/ask RPC.
type QuestionModal struct {
	Questions []QuestionItem
	Index     int // current question being answered
	Answers   []string
	width     int
}

// QuestionItem is one question shown in the modal.
type QuestionItem struct {
	Question string
	Options  []string
}

// NewQuestionModal creates a modal for the given questions.
func NewQuestionModal(questions []QuestionItem) *QuestionModal {
	return &QuestionModal{Questions: questions}
}

// SetSize updates the modal width.
func (m *QuestionModal) SetSize(width int) { m.width = width }

// Current returns the question index and text being shown.
func (m *QuestionModal) Current() (int, QuestionItem) {
	if m.Index >= len(m.Questions) {
		return m.Index, QuestionItem{}
	}
	return m.Index, m.Questions[m.Index]
}

// Advance records an answer and moves to the next question.
// Returns true when all questions are answered.
func (m *QuestionModal) Advance(answer string) bool {
	m.Answers = append(m.Answers, answer)
	m.Index++
	return m.Index >= len(m.Questions)
}

// Render returns a styled question prompt string.
func (m *QuestionModal) Render() string {
	t := theme.CurrentTheme()
	_, q := m.Current()
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Padding(0, 2).
		Width(m.width - 4)

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(fmt.Sprintf("? Вопрос агента (%d/%d)", m.Index+1, len(m.Questions)))

	body := q.Question
	if len(q.Options) > 0 {
		body += "\n\n"
		for i, opt := range q.Options {
			body += fmt.Sprintf("  %d) %s\n", i+1, opt)
		}
		body += "\nВведите номер или текст, затем Enter"
	} else {
		body += "\n\nВведите ответ и нажмите Enter"
	}
	return border.Render(fmt.Sprintf("%s\n\n%s", title, body))
}

// Render returns a styled permission prompt string.
func (m *Modal) Render() string {
	t := theme.CurrentTheme()
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Warning()).
		Padding(0, 2).
		Width(m.width - 4)

	title := lipgloss.NewStyle().
		Foreground(t.Warning()).
		Bold(true).
		Render("⚠ Разрешение shell")

	desc := m.Description
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}
	tool := m.Tool
	if tool == "" {
		tool = "shell"
	}
	body := fmt.Sprintf("%s\n\nИнструмент: %s\nКоманда: %s\n\n[y] один раз   [a] на сессию   [t] этот tool   [n] запретить",
		title, tool, desc)
	return border.Render(body)
}

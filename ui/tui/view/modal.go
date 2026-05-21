package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Modal displays a blocking yes/no dialog for exec.run consent.
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
	Index     int   // current question being answered
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
	_, q := m.Current()
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7aa2f7")).
		Padding(0, 2).
		Width(m.width - 4)

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7aa2f7")).
		Bold(true).
		Render(fmt.Sprintf("? Agent question (%d/%d)", m.Index+1, len(m.Questions)))

	body := q.Question
	if len(q.Options) > 0 {
		body += "\n\n"
		for i, opt := range q.Options {
			body += fmt.Sprintf("  %d) %s\n", i+1, opt)
		}
		body += "\nEnter number or text, then press Enter"
	} else {
		body += "\n\nType your answer and press Enter"
	}
	return border.Render(fmt.Sprintf("%s\n\n%s", title, body))
}

// Render returns a styled permission prompt string.
func (m *Modal) Render() string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(0, 2).
		Width(m.width - 4)

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f7768e")).
		Bold(true).
		Render("⚠ exec.run permission request")

	desc := m.Description
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}
	body := fmt.Sprintf("%s\n\nCommand: %s\n\n[y] Allow   [n] Deny", title, desc)
	return border.Render(body)
}

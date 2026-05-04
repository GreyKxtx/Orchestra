package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// QuestionItem is a single question the agent can ask the user.
type QuestionItem struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"` // nil = free text
}

// QuestionAsker blocks execution until the user provides answers.
// Implemented by StdinQuestionAsker (direct CLI mode) or a future RPCQuestionAsker (core).
// nil QuestionAsker means the question tool is unavailable.
type QuestionAsker interface {
	Ask(ctx context.Context, questions []QuestionItem) ([]string, error)
}

// StdinQuestionAsker reads answers from os.Stdin.
// Only use in direct CLI mode — NOT in orchestra core (would corrupt stdio JSON-RPC stream).
type StdinQuestionAsker struct{}

func (s *StdinQuestionAsker) Ask(_ context.Context, questions []QuestionItem) ([]string, error) {
	scanner := bufio.NewScanner(os.Stdin)
	answers := make([]string, 0, len(questions))
	for _, q := range questions {
		fmt.Fprintf(os.Stderr, "\n[Агент спрашивает] %s\n", q.Question)
		if len(q.Options) > 0 {
			for i, opt := range q.Options {
				fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, opt)
			}
			fmt.Fprint(os.Stderr, "Введите номер варианта или текст: ")
		} else {
			fmt.Fprint(os.Stderr, "Ответ: ")
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("stdin closed")
		}
		answers = append(answers, strings.TrimSpace(scanner.Text()))
	}
	return answers, nil
}

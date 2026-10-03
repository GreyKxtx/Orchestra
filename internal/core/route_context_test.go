package core

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestEarlierExchange_IsTheLastQuestionAndItsAnswer(t *testing.T) {
	ui := []sessionfile.UIMessage{
		{Role: "user", Text: "привет"},
		{Role: "assistant", Text: "Привет! Чем помочь?"},
		{Role: "user", Text: "сделай игру змейка"},
		{Role: "system", SystemKind: "info", Text: "mode: build"},
		{Role: "assistant", Text: "Готово — index.html и game.js. Добавить таблицу рекордов?"},
	}
	got := earlierExchange(ui)
	for _, want := range []string{"user: сделай игру змейка", "assistant: Готово", "таблицу рекордов?"} {
		if !strings.Contains(got, want) {
			t.Errorf("earlierExchange = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "привет") || strings.Contains(got, "mode: build") {
		t.Errorf("earlierExchange = %q, want only the last exchange", got)
	}
	if earlierExchange(nil) != "" {
		t.Error("a new session has no earlier exchange")
	}
}

// A long answer keeps its end: that is where the assistant asks its question.
func TestEarlierExchange_KeepsTheEndOfALongAnswer(t *testing.T) {
	long := strings.Repeat("код ", 400) + "Какой цвет змейки?"
	got := earlierExchange([]sessionfile.UIMessage{{Role: "user", Text: "сделай"}, {Role: "assistant", Text: long}})
	if !strings.HasSuffix(got, "Какой цвет змейки?") || len([]rune(got)) > 2*earlierExchangeMaxRunes {
		t.Fatalf("earlierExchange kept %d runes, ending %q", len([]rune(got)), got[len(got)-40:])
	}
}

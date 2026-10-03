package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// earlierExchangeMaxRunes bounds each side of the exchange the auto-router is
// shown: enough to say what the work is, small for a router model.
const earlierExchangeMaxRunes = 600

// earlierExchange is the session's last user message and the answer to it, as
// the user saw them, for the auto-router. The chat projection, not the LLM
// history: that one also holds the runtime's own notes to the model.
//
// The answer keeps its end — where an assistant puts its question to the user
// — and the user's message its beginning.
func earlierExchange(ui []sessionfile.UIMessage) string {
	var user, assistant string
	for i := len(ui) - 1; i >= 0; i-- {
		text := strings.TrimSpace(ui[i].Text)
		if text == "" {
			continue
		}
		switch ui[i].Role {
		case "assistant":
			if assistant == "" && user == "" {
				assistant = text
			}
		case "user":
			user = text
		}
		if user != "" {
			break
		}
	}
	var b strings.Builder
	if user != "" {
		b.WriteString("user: " + headRunes(user, earlierExchangeMaxRunes))
	}
	if assistant != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("assistant: " + tailRunes(assistant, earlierExchangeMaxRunes))
	}
	return b.String()
}

func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

package agent

import (
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// interjectionTag wraps a message the user sent while the turn ran. Not
// <user_query>: that tag marks the turn's own request, and the loop finds the
// newest one to decide where the query already stands in the history.
const interjectionTag = "user_message"

// interjectionMessage is how one such message reaches the model: the user's
// words, marked as theirs and as arriving mid-work.
func interjectionMessage(text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: "The user sent this while you were working. It is from the user, not a tool: take it into account from this step on — it may change or add to the task.\n" +
		"<" + interjectionTag + ">\n" + strings.TrimSpace(text) + "\n</" + interjectionTag + ">"}
}

// takeInterjections appends what the user sent since the last look. final is
// the final step's look (see Options.Interjections).
func (l *turnLoop) takeInterjections(history []llm.Message, final bool) []llm.Message {
	if l.a.opts.Interjections == nil {
		return history
	}
	for _, text := range l.a.opts.Interjections(final) {
		if strings.TrimSpace(text) == "" {
			continue
		}
		history = append(history, interjectionMessage(text))
	}
	return history
}

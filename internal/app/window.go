package app

import (
	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// FitWindow sizes an agent to the model it actually runs on: that client's
// context window, and a history budget that fills it (the window less the
// answer's reserve, as llm.PromptBudgetTokens keeps it).
//
// The window came from llm.num_ctx alone — a local server's setting. An
// orchestra turn on Claude Opus, with the project's llm: still naming a 32K
// local model, measured itself against 32K; with no num_ctx at all, the
// history budget was limits.context_kb (50 KB, about 12K tokens), and a 200K
// model announced "context nearly full" after a few file reads. The budget
// only grows here: a config that asks for more keeps it.
func FitWindow(o *agent.Options, client llm.Client) {
	if o == nil || client == nil {
		return
	}
	w := llm.ContextTokensOf(client)
	if w <= 0 {
		return
	}
	o.ModelContextTokens = w
	reserve := o.CompletionMaxTokens
	if reserve <= 0 {
		reserve = 4096
	}
	promptTok := w - reserve - 2048
	if floor := w / 4; promptTok < floor {
		promptTok = floor
	}
	bpt := o.BytesPerContextToken
	if bpt <= 0 {
		bpt = 4
	}
	if b := promptTok * bpt; b > o.MaxPromptBytes {
		o.MaxPromptBytes = b
	}
}

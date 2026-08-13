package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/llm"
)

// ctxCategory is one row of the context-usage breakdown shown in client
// popovers (VS Code context ring, TUI ctx bar). Tokens are estimates derived
// from byte sizes with the same bytes-per-token factor as the total.
type ctxCategory struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Tokens int    `json:"tokens"`
}

// contextBreakdownFixed returns the per-category sizes of everything that is
// constant for the agent's lifetime: system prompt, injected memory (rules),
// tool definitions (catalog block + JSON schemas) and the skills block.
func (a *Agent) contextBreakdownFixed() []ctxCategory {
	a.ctxBreakdownOnce.Do(func() {
		bpt := a.bytesPerToken()
		if bpt <= 0 {
			bpt = DefaultBytesPerContextToken
		}
		parts := a.buildSystemPromptParts()
		toolBytes := len(parts.catalog)
		if b, err := json.Marshal(a.buildToolDefs()); err == nil {
			toolBytes += len(b)
		}
		cats := []ctxCategory{
			{Key: "system", Label: "System prompt", Tokens: len(parts.base) / bpt},
			{Key: "tools", Label: "Tool definitions", Tokens: toolBytes / bpt},
		}
		if len(parts.memory) > 0 {
			cats = append(cats, ctxCategory{Key: "rules", Label: "Memory & rules", Tokens: len(parts.memory) / bpt})
		}
		if len(parts.skills) > 0 {
			cats = append(cats, ctxCategory{Key: "skills", Label: "Skills", Tokens: len(parts.skills) / bpt})
		}
		a.ctxBreakdownFixed = cats
	})
	return a.ctxBreakdownFixed
}

// contextBreakdown appends the live conversation size to the fixed categories.
func (a *Agent) contextBreakdown(history []llm.Message) []ctxCategory {
	bpt := a.bytesPerToken()
	if bpt <= 0 {
		bpt = DefaultBytesPerContextToken
	}
	fixed := a.contextBreakdownFixed()
	out := make([]ctxCategory, 0, len(fixed)+1)
	out = append(out, fixed...)
	out = append(out, ctxCategory{
		Key:    "conversation",
		Label:  "Conversation",
		Tokens: historyBytes(history) / bpt,
	})
	return out
}

func (a *Agent) emitPromptContextEstimate(step int, history []llm.Message) {
	if a.opts.OnEvent == nil {
		return
	}
	breakdown := a.contextBreakdown(history)
	est := estimatePromptTokensWithFactor(history, a.opts.MaxPromptBytes, a.bytesPerToken())
	// The category sum is a lower bound on the real prompt; keep the total
	// consistent with the breakdown so the bar never overflows its segments.
	sum := 0
	for _, c := range breakdown {
		sum += c.Tokens
	}
	if sum > est {
		est = sum
	}
	// Prefer last real usage as a floor — history only grows within a turn,
	// and a fresh Agent after reopen has lastPromptTokens=0 until usage returns.
	if a.lastPromptTokens > est {
		est = a.lastPromptTokens
	}
	if est <= 0 {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"prompt_tokens": est,
		"source":        "estimate",
		"breakdown":     breakdown,
	})
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepUsage,
		Content: string(payload),
	}})
}

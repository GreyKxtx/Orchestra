package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/llm"
)

func (a *Agent) emitPromptContextEstimate(step int, history []llm.Message) {
	if a.opts.OnEvent == nil {
		return
	}
	est := estimatePromptTokensWithFactor(history, a.opts.MaxPromptBytes, a.bytesPerToken())
	if est <= 0 {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"prompt_tokens": est,
		"source":        "estimate",
	})
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepUsage,
		Content: string(payload),
	}})
}

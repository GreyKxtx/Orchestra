package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/llm"
)

func (a *Agent) emitPromptContextEstimate(step int, history []llm.Message) {
	if a.opts.OnEvent == nil {
		return
	}
	est := estimatePromptTokens(history, a.opts.MaxPromptBytes)
	if est <= 0 {
		return
	}
	payload, _ := json.Marshal(map[string]int{
		"prompt_tokens": est,
	})
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepUsage,
		Content: string(payload),
	}})
}

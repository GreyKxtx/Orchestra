package agent

import (
	"strings"

	"github.com/orchestra/orchestra/llm"
)

func (a *Agent) messagesWithAssistantPrefill(messages []llm.Message) []llm.Message {
	prefill := strings.TrimSpace(a.opts.AssistantPrefill)
	if prefill == "" {
		return messages
	}
	out := make([]llm.Message, len(messages), len(messages)+1)
	copy(out, messages)
	return append(out, llm.Message{Role: llm.RoleAssistant, Content: prefill})
}

func mergeAssistantPrefill(prefill string, msg llm.Message) llm.Message {
	prefill = strings.TrimSpace(prefill)
	if prefill == "" || strings.TrimSpace(msg.Content) == "" {
		return msg
	}
	if !strings.HasPrefix(msg.Content, prefill) {
		msg.Content = prefill + msg.Content
	}
	return msg
}

func (a *Agent) mergeResponsePrefill(resp *llm.CompleteResponse) {
	if resp == nil {
		return
	}
	resp.Message = mergeAssistantPrefill(a.opts.AssistantPrefill, resp.Message)
}

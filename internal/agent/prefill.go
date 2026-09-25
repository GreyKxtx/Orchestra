package agent

import (
	"strings"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/llm"
)

// messagesWithAssistantPrefill appends the prefill as a partial assistant
// message — only when the request offers no tools. With tools, a "{" start
// pushes the model into JSON text, so the worker it was set for could not call
// edit or write; and recent Claude models reject a request that ends with an
// assistant message outright.
func (a *Agent) messagesWithAssistantPrefill(messages []llm.Message, hasTools bool) []llm.Message {
	prefill := strings.TrimSpace(a.opts.AssistantPrefill)
	a.prefillSent = prefill != "" && !hasTools
	if !a.prefillSent {
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
	if !a.prefillSent {
		return
	}
	resp.Message = mergeAssistantPrefill(a.opts.AssistantPrefill, resp.Message)
}

// maxStepsReminder is the step-limit warning in the words that fit this run:
// a child ends with task_result, not a PatchSet.
func (a *Agent) maxStepsReminder() string {
	if a.opts.IsChild && a.offersTool("task_result") {
		return "Few steps remain before the limit. Wrap up now: call task_result with what you have done and what is left."
	}
	return promptpkg.MaxStepsReminder
}

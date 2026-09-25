package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestMergeAssistantPrefill(t *testing.T) {
	msg := llm.Message{Role: llm.RoleAssistant, Content: `"type":"final"`}
	got := mergeAssistantPrefill("{", msg)
	if got.Content != `{"type":"final"` {
		t.Fatalf("got %q", got.Content)
	}
}

func TestMessagesWithAssistantPrefill(t *testing.T) {
	a := &Agent{opts: Options{AssistantPrefill: "{"}}
	msgs := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	got := a.messagesWithAssistantPrefill(msgs, false)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[1].Role != llm.RoleAssistant || got[1].Content != "{" {
		t.Fatalf("prefill message: %+v", got[1])
	}
}

// With tools on offer the prefill is not sent (it pushes the model into JSON
// text instead of a tool call), and a response is not merged with a prefill
// its request did not carry.
func TestAssistantPrefillOnlyWithoutTools(t *testing.T) {
	a := &Agent{opts: Options{AssistantPrefill: "{"}}
	msgs := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	if got := a.messagesWithAssistantPrefill(msgs, true); len(got) != 1 {
		t.Fatalf("a request with tools must end with the user message, got %+v", got)
	}
	resp := &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `"x":1}`}}
	a.mergeResponsePrefill(resp)
	if resp.Message.Content != `"x":1}` {
		t.Fatalf("nothing was prefilled, nothing is merged: %q", resp.Message.Content)
	}
}

func TestMaxStepsReminderIsAUserMessageForTheRun(t *testing.T) {
	llmClient := &toolCallSequenceLLM{}
	ag, _ := newTestAgent(t, llmClient, Options{Mode: ModeWorker, IsChild: true})
	if got := ag.maxStepsReminder(); !strings.Contains(got, "task_result") {
		t.Fatalf("a child wraps up with task_result: %q", got)
	}
}

package agent

import (
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
	got := a.messagesWithAssistantPrefill(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[1].Role != llm.RoleAssistant || got[1].Content != "{" {
		t.Fatalf("prefill message: %+v", got[1])
	}
}

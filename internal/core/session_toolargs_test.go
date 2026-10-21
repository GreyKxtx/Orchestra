package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
)

// A Claude turn's tool blocks were saved without their arguments (the
// argument deltas named no call, and both hosts dropped them): a reopened
// chat showed every subagent as "agent · Task". The session's history keeps
// the calls whole; session.get fills a block saved without arguments from
// the call of the same id, and leaves the saved ones and the stored record
// alone.
func TestWithToolArgs_FillsBlocksSavedWithoutArguments(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "what next"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_1", Type: "function", Function: llm.ToolCallFunc{Name: "task", Arguments: llm.ToolArguments(`{"subagent_type":"product","description":"Draft the PRD"}`)}},
			{ID: "toolu_2", Type: "function", Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"a.go"}`)}},
		}},
	}
	stored := []sessionfile.UIMessage{
		{Role: "assistant", ToolBlocks: []sessionfile.UIToolBlock{{ID: "toolu_1", Name: "task"}, {ID: "toolu_2", Name: "read", ArgsRaw: `{"path":"kept.go"}`}}},
		{Role: "assistant", Segments: []sessionfile.UISegment{{Kind: "tools", Tools: []sessionfile.UIToolBlock{{ID: "toolu_1", Name: "task"}}}}},
	}
	ui := append([]sessionfile.UIMessage(nil), stored...)
	got := withToolArgs(ui, history)

	if a := got[0].ToolBlocks[0].ArgsRaw; a != `{"subagent_type":"product","description":"Draft the PRD"}` {
		t.Fatalf("task block args = %q", a)
	}
	if a := got[0].ToolBlocks[1].ArgsRaw; a != `{"path":"kept.go"}` {
		t.Fatalf("saved args were replaced: %q", a)
	}
	if a := got[1].Segments[0].Tools[0].ArgsRaw; a == "" {
		t.Fatal("a segment's tool block was not filled")
	}
	if stored[0].ToolBlocks[0].ArgsRaw != "" || stored[1].Segments[0].Tools[0].ArgsRaw != "" {
		t.Fatal("the session's own record was written to")
	}
}

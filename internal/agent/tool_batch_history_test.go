package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// replyOrderLLM answers from a list of responses and keeps every request.
type replyOrderLLM struct {
	responses []*llm.CompleteResponse
	i         int
	seen      [][]llm.Message
}

func (s *replyOrderLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *replyOrderLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.seen = append(s.seen, append([]llm.Message(nil), req.Messages...))
	if s.i >= len(s.responses) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	r := s.responses[s.i]
	s.i++
	return r, nil
}

func writeCall(id, path string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunc{
		Name:      "write",
		Arguments: llm.ToolArguments([]byte(`{"path":"` + path + `","content":"x\n"}`)),
	}}
}

// assertRepliesFollowCalls fails when a tool_call is not answered before the
// next non-tool message, or a tool reply answers no open call — the shapes a
// provider rejects with 400.
func assertRepliesFollowCalls(t *testing.T, msgs []llm.Message) {
	t.Helper()
	open := map[string]bool{}
	for i, m := range msgs {
		switch {
		case m.Role == llm.RoleTool:
			if !open[m.ToolCallID] {
				t.Fatalf("message %d: tool reply %q answers no open call\n%+v", i, m.ToolCallID, msgs)
			}
			delete(open, m.ToolCallID)
		default:
			if len(open) > 0 {
				t.Fatalf("message %d (%s) arrives while calls %v are unanswered\n%+v", i, m.Role, open, msgs)
			}
			if m.Role == llm.RoleAssistant {
				for _, tc := range m.ToolCalls {
					open[tc.ID] = true
				}
			}
		}
	}
}

// Two writes in one response in a dry-run: the first earns a "staged ready"
// hint. The hint used to land between the two replies, which split the batch
// and orphaned the second reply on every later request of the session.
func TestSerialBatch_HintsWaitForEveryReply(t *testing.T) {
	llmClient := &replyOrderLLM{responses: []*llm.CompleteResponse{{
		Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			writeCall("c1", "n1.txt"), writeCall("c2", "n2.txt"),
		}},
	}}}
	ag, tr := newTestAgent(t, llmClient, Options{Mode: ModeBuild})
	tr.SetDryRun(true)

	hist, _, err := ag.Run(context.Background(), nil, "write two files")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(llmClient.seen) < 2 {
		t.Fatalf("expected a second request after the batch, got %d", len(llmClient.seen))
	}
	second := llmClient.seen[1]
	assertRepliesFollowCalls(t, second)
	assertRepliesFollowCalls(t, hist)

	var hinted bool
	for _, m := range second {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "Staged changes for") {
			hinted = true
		}
	}
	if !hinted {
		t.Fatal("the staged-ready hint must still reach the model, after the replies")
	}
}

// A provider that omits tool_call ids: the assistant message and its replies
// must still pair up.
func TestToolCallsWithoutIDsStillPair(t *testing.T) {
	llmClient := &replyOrderLLM{responses: []*llm.CompleteResponse{{
		Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{Type: "function", Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.txt"}`))}},
			{Type: "function", Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))}},
		}},
	}}}
	ag, _ := newTestAgent(t, llmClient, Options{Mode: ModeBuild})
	if _, _, err := ag.Run(context.Background(), nil, "look around"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(llmClient.seen) < 2 {
		t.Fatalf("expected a second request, got %d", len(llmClient.seen))
	}
	second := llmClient.seen[1]
	assertRepliesFollowCalls(t, second)
	var replies int
	for _, m := range second {
		if m.Role == llm.RoleTool {
			replies++
		}
	}
	if replies != 2 {
		t.Fatalf("both replies must survive, got %d:\n%+v", replies, second)
	}
}

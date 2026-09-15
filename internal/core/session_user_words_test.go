package core

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// turnRecordingLLM keeps every request and answers each with a short final.
type turnRecordingLLM struct {
	mu   sync.Mutex
	reqs [][]llm.Message
}

func (r *turnRecordingLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (r *turnRecordingLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, append([]llm.Message(nil), req.Messages...))
	r.mu.Unlock()
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "OK"}}, nil
}

// A chat turn's own message never entered the session history: the agent
// rebuilds the current query into every request, and only its replies and
// tool calls were kept. So the next turn could not see what the user had said
// before. Seen live on qwen3.5-9b: "My favourite number is 7342. Reply OK",
// then "What is my favourite number?" — the second request held the model's
// "OK" and no 7342, and the model said no number had been mentioned. Every
// follow-up in VS Code, the TUI and the web UI lost its antecedent.
func TestSession_TheNextTurnSeesWhatTheUserSaidBefore(t *testing.T) {
	client := &turnRecordingLLM{}
	c := newChatCore(t, client)
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"My favourite number is 7342. Reply with just OK.",
		"What is my favourite number?",
	} {
		if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
			SessionID: started.SessionID, Content: text, Mode: "build",
		}); err != nil {
			t.Fatal(err)
		}
	}

	client.mu.Lock()
	last := client.reqs[len(client.reqs)-1]
	client.mu.Unlock()
	var sawEarlier, earlierBeforeReply bool
	replySeen := false
	for _, m := range last {
		if m.Role == llm.RoleAssistant && strings.TrimSpace(m.Content) == "OK" {
			replySeen = true
		}
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "7342") && !strings.Contains(m.Content, "What is my favourite number?") {
			sawEarlier = true
			earlierBeforeReply = !replySeen
		}
	}
	if !sawEarlier {
		t.Fatal("the second turn's request does not carry what the user said in the first turn")
	}
	if !earlierBeforeReply {
		t.Error("the first turn's message comes after the reply to it")
	}
}

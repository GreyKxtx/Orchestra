package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/wire"
)

// interjectLLM reads a file on its first step and holds there until the test
// lets it go; afterwards it finishes, recording what each step was given.
type interjectLLM struct {
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	saw     []string
}

func (s *interjectLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *interjectLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	var said strings.Builder
	steps := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
		if m.Role == llm.RoleUser {
			said.WriteString(m.Content + "\n")
		}
	}
	s.mu.Lock()
	s.saw = append(s.saw, said.String())
	s.mu.Unlock()
	if steps == 0 {
		s.once.Do(func() { close(s.entered) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return toolCall("read", `{"path":"a.txt"}`), nil
	}
	return finalText("done"), nil
}

// A message sent while the turn works reaches the model at its next step: the
// turn is not cancelled, the client hears when the model got it, and the chat
// keeps it where it was said.
func TestSessionInterject_ReachesTheRunningTurn(t *testing.T) {
	root := resumeWorkspace(t)
	model := &interjectLLM{entered: make(chan struct{}), release: make(chan struct{})}
	c, err := New(root, Options{LLMClient: model})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := start.SessionID

	if res, err := c.SessionInterject(wire.SessionInterjectParams{SessionID: sid, Content: "too early"}); err != nil || res.Accepted {
		t.Fatalf("with no turn running the message must be refused: %+v, %v", res, err)
	}

	var evMu sync.Mutex
	var delivered []wire.AgentEvent
	onEvent := func(method string, params any) {
		if ev, ok := params.(wire.AgentEvent); ok && ev.Type == wire.EventUserMessage {
			evMu.Lock()
			delivered = append(delivered, ev)
			evMu.Unlock()
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "look at a.txt", Mode: "ask", OnEvent: onEvent})
		done <- err
	}()
	select {
	case <-model.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	res, err := c.SessionInterject(wire.SessionInterjectParams{SessionID: sid, Content: "and count its lines"})
	if err != nil || !res.Accepted || res.ID == "" {
		t.Fatalf("the running turn must take the message: %+v, %v", res, err)
	}
	close(model.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the turn never ended")
	}

	model.mu.Lock()
	saw := append([]string(nil), model.saw...)
	model.mu.Unlock()
	if len(saw) < 2 || !strings.Contains(saw[1], "<user_message>") || !strings.Contains(saw[1], "and count its lines") {
		t.Fatalf("the second step did not carry the message: %q", saw)
	}
	evMu.Lock()
	got := append([]wire.AgentEvent(nil), delivered...)
	evMu.Unlock()
	if len(got) != 1 || got[0].Content != "and count its lines" {
		t.Fatalf("user_message events = %+v", got)
	}
	if data, ok := got[0].Data.(wire.UserMessage); !ok || data.ID != res.ID {
		t.Fatalf("user_message carries the id session.interject gave: %+v, want %q", got[0].Data, res.ID)
	}

	view, err := c.SessionGet(SessionGetParams{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range view.UIMessages {
		if m.SystemKind == "interjection" && m.Text == "and count its lines" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the chat lost the message: %+v", view.UIMessages)
	}

	if res, _ := c.SessionInterject(wire.SessionInterjectParams{SessionID: sid, Content: "late"}); res.Accepted {
		t.Fatal("a finished turn must refuse, so the client sends the message as a turn")
	}
}

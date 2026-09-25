package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// gatedLLM answers at once, except a request whose last user message says
// "hold": that one waits until the gate opens, and reports that it is
// waiting. It stands in for a slow model in one session while another
// session asks for something quick.
type gatedLLM struct {
	gate    chan struct{}
	waiting chan struct{}
	once    sync.Once
}

func (g *gatedLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	final := &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != llm.RoleUser {
			continue
		}
		if strings.Contains(m.Content, "hold") {
			g.once.Do(func() { close(g.waiting) })
			select {
			case <-g.gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		break
	}
	return final, nil
}

func (g *gatedLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

// Two sessions of one core run their turns at the same time: a turn used to
// hold the core's one mutex for its whole length, so every other session
// waited on the slowest model (ARCH-5).
func TestSessionMessage_TwoSessionsRunAtOnce(t *testing.T) {
	root := t.TempDir()
	model := &gatedLLM{gate: make(chan struct{}), waiting: make(chan struct{})}
	_, h := setupInitializedCore(t, root, model)

	start := func() string {
		t.Helper()
		p, _ := json.Marshal(SessionStartParams{})
		res, err := h.Handle(context.Background(), "session.start", p)
		if err != nil {
			t.Fatalf("session.start: %v", err)
		}
		return res.(*SessionStartResult).SessionID
	}
	slow, quick := start(), start()

	slowDone := make(chan error, 1)
	go func() {
		p, _ := json.Marshal(SessionMessageParams{SessionID: slow, Content: "hold this turn", MaxSteps: 2})
		_, err := h.Handle(context.Background(), "session.message", p)
		slowDone <- err
	}()
	select {
	case <-model.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow session's turn never reached the model")
	}

	quickDone := make(chan error, 1)
	go func() {
		p, _ := json.Marshal(SessionMessageParams{SessionID: quick, Content: "answer now", MaxSteps: 2})
		_, err := h.Handle(context.Background(), "session.message", p)
		quickDone <- err
	}()
	select {
	case err := <-quickDone:
		if err != nil {
			t.Fatalf("the quick session's turn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the quick session's turn waited on the slow session's turn")
	}

	close(model.gate)
	select {
	case err := <-slowDone:
		if err != nil {
			t.Fatalf("the slow session's turn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the slow session's turn never finished after the model answered")
	}
}

// perSessionLLM answers the first request of each session with a write of
// that session's file and every later one with a final: the core has one
// model client, and the sessions must not share a script.
type perSessionLLM struct {
	mu    sync.Mutex
	calls map[string]int
}

func (p *perSessionLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	key := ""
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser {
			continue
		}
		for _, k := range []string{"alpha", "beta"} {
			if strings.Contains(m.Content, k) {
				key = k
			}
		}
	}
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	n := p.calls[key]
	p.calls[key]++
	p.mu.Unlock()
	if key != "" && n == 0 {
		step := `{"type":"tool_call","tool":{"name":"write","input":{"path":"` + key + `.txt","content":"from ` + key + `\n"}}}`
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: step}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func (p *perSessionLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

// A session's staged edits are its own: a turn of another session neither
// sees nor drops them, and apply_pending applies the right session's.
func TestSessionMessage_StagingIsPerSession(t *testing.T) {
	root := t.TempDir()
	c, h := setupInitializedCore(t, root, &perSessionLLM{})

	start := func() string {
		t.Helper()
		p, _ := json.Marshal(SessionStartParams{})
		res, err := h.Handle(context.Background(), "session.start", p)
		if err != nil {
			t.Fatalf("session.start: %v", err)
		}
		return res.(*SessionStartResult).SessionID
	}
	a, b := start(), start()
	for id, word := range map[string]string{a: "alpha", b: "beta"} {
		p, _ := json.Marshal(SessionMessageParams{SessionID: id, Content: "write the " + word + " file", MaxSteps: 3})
		if _, err := h.Handle(context.Background(), "session.message", p); err != nil {
			t.Fatalf("session.message %s: %v", id, err)
		}
	}
	sessA, _ := c.sessions.Get(a)
	sessB, _ := c.sessions.Get(b)
	if got := sessA.Turn(c.tools).ListStagedPaths(); len(got) != 1 || got[0] != "alpha.txt" {
		t.Fatalf("session a staged %v, want [alpha.txt]: session b's turn touched it", got)
	}
	if got := sessB.Turn(c.tools).ListStagedPaths(); len(got) != 1 || got[0] != "beta.txt" {
		t.Fatalf("session b staged %v, want [beta.txt]", got)
	}
	if c.tools.HasStagedChanges() {
		t.Fatal("the core's default turn holds a session's edits")
	}

	p, _ := json.Marshal(SessionApplyPendingParams{SessionID: b})
	res, err := h.Handle(context.Background(), "session.apply_pending", p)
	if err != nil {
		t.Fatalf("apply_pending: %v", err)
	}
	applied := res.(*SessionApplyPendingResult)
	if !applied.Applied || len(applied.ApplyResponse.ChangedFiles) != 1 || applied.ApplyResponse.ChangedFiles[0] != "beta.txt" {
		t.Fatalf("apply_pending for b applied %+v, want beta.txt only", applied.ApplyResponse)
	}
	if !sessA.Turn(c.tools).HasStagedChanges() {
		t.Fatal("applying session b's edits dropped session a's staged edit")
	}
	if _, err := os.Stat(filepath.Join(root, "alpha.txt")); !os.IsNotExist(err) {
		t.Fatal("session a's preview reached the disk")
	}
}

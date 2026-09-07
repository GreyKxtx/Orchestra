package core

import (
	"context"
	"sync"

	"github.com/orchestra/orchestra/internal/tools"
)

// rpcQuestionAsker routes question prompts through the server-initiated
// request function the RPC handler injects (method "question/ask").
//
// Concurrent callers are serialized (FIFO), like rpcPermissionRequester.
// One source used to exist — the agent's question tool, inside a turn — but
// MCP elicitation now asks from a server goroutine, so two can overlap. The
// clients cannot take that: the TUI holds a single questionModal and a single
// questionReqID (ui/tui/app.go:90-91), so a second request overwrites the
// first and the first's caller waits on an answer nobody can send any more.
// A person answers one dialog at a time regardless; the queue makes it
// explicit instead of dropping one.
type rpcQuestionAsker struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
	mu        sync.Mutex
}

func (r *rpcQuestionAsker) Ask(ctx context.Context, questions []tools.QuestionItem) ([]string, error) {
	if r.requestFn == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var resp struct {
		Answers []string `json:"answers"`
	}
	req := map[string]any{"questions": questions}
	if err := r.requestFn(ctx, "question/ask", req, &resp); err != nil {
		return nil, err
	}
	return resp.Answers, nil
}

package core

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/orchestra/orchestra/protocol"
)

// rpcBrowserPanel is the client's own browser, reached the way questions and
// permission prompts are reached: a server-initiated request on the connection
// that asked for the turn (method "browser/call").
//
// Nothing here knows what a browser is. The client is told an op and its
// arguments and answers with a result object; refusing is a normal answer —
// the panel may be closed, or the person may have taken the permission back
// mid-turn, and the model is told so in the tool's error.
//
// Serialized, like rpcQuestionAsker: a turn's tool calls are sequential
// anyway, but subagents are not, and one window drives one browser. Two ops
// interleaved on the same page would be a race over what the page is showing.
type rpcBrowserPanel struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
	mu        sync.Mutex
}

func (r *rpcBrowserPanel) Call(ctx context.Context, op string, params map[string]any) (json.RawMessage, error) {
	if r == nil || r.requestFn == nil {
		return nil, protocol.NewError(protocol.ExecDenied,
			"no client is attached to answer for the browser panel", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if params == nil {
		params = map[string]any{}
	}
	var result json.RawMessage
	req := map[string]any{"op": op, "params": params}
	if err := r.requestFn(ctx, "browser/call", req, &result); err != nil {
		return nil, err
	}
	// A client's reply channel carries results only, so a refusal arrives as
	// one: `{"error": "the browser view is not open"}`. It is the model's
	// answer, and the reason is the useful part of it.
	var refusal struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(result, &refusal); err == nil && refusal.Error != "" {
		return nil, protocol.NewError(protocol.ExecFailed, refusal.Error, map[string]any{"op": op})
	}
	return result, nil
}

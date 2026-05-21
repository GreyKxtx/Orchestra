package core

import (
	"context"

	"github.com/orchestra/orchestra/internal/tools"
)

// rpcQuestionAsker routes question prompts through the server-initiated
// request function the RPC handler injects (method "question/ask").
type rpcQuestionAsker struct {
	requestFn func(ctx context.Context, method string, params any, result any) error
}

func (r *rpcQuestionAsker) Ask(ctx context.Context, questions []tools.QuestionItem) ([]string, error) {
	if r.requestFn == nil {
		return nil, nil
	}
	var resp struct {
		Answers []string `json:"answers"`
	}
	req := map[string]any{"questions": questions}
	if err := r.requestFn(ctx, "question/ask", req, &resp); err != nil {
		return nil, err
	}
	return resp.Answers, nil
}

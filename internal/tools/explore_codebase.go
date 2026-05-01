package tools

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/ckg"
)

type ExploreCodebaseRequest struct {
	SymbolName string `json:"symbol_name"`
}

type ExploreCodebaseResponse struct {
	Content string `json:"content"`
}

func (r *Runner) ExploreCodebase(ctx context.Context, req ExploreCodebaseRequest) (*ExploreCodebaseResponse, error) {
	if r.ckgStore == nil || r.ckgProvider == nil {
		return nil, fmt.Errorf("ckg store not initialized")
	}

	// Update graph incrementally on every call (millisecond if no changes).
	orch := ckg.NewOrchestrator(r.ckgStore, r.workspaceRoot)
	if err := orch.UpdateGraph(ctx); err != nil {
		return nil, fmt.Errorf("update ckg: %w", err)
	}

	content, err := r.ckgProvider.ExploreSymbol(ctx, req.SymbolName)
	if err != nil {
		return nil, fmt.Errorf("explore symbol: %w", err)
	}
	return &ExploreCodebaseResponse{Content: content}, nil
}

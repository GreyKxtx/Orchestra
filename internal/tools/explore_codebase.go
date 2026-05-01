package tools

import (
	"context"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/internal/ckg"
)

type ExploreCodebaseRequest struct {
	SymbolName string `json:"symbol_name"`
}

type ExploreCodebaseResponse struct {
	Content string `json:"content"`
}

// ExploreCodebase provides context for a specific symbol using the Code Knowledge Graph.
// It automatically updates the incremental graph before querying.
func (r *Runner) ExploreCodebase(ctx context.Context, req ExploreCodebaseRequest) (*ExploreCodebaseResponse, error) {
	orchDir := filepath.Join(r.workspaceRoot, ".orchestra")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(orchDir, "ckg.db")
	
	store, err := ckg.NewStore("file:" + dbPath + "?cache=shared")
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Update graph incrementally on every call.
	// Since the scanner uses file hashing, this takes milliseconds
	// if there are no changes, guaranteeing fresh context.
	orch := ckg.NewOrchestrator(store, r.workspaceRoot)
	if err := orch.UpdateGraph(ctx); err != nil {
		return nil, err
	}

	provider := ckg.NewProvider(store, r.workspaceRoot)
	content, err := provider.ExploreSymbol(ctx, req.SymbolName)
	if err != nil {
		return nil, err
	}

	return &ExploreCodebaseResponse{Content: content}, nil
}

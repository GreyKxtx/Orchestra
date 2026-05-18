package tools

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/repomap"
)

// RepoMapRequest is the JSON input for the repo_map tool.
type RepoMapRequest struct {
	BudgetBytes int `json:"budget_bytes,omitempty"`
	MaxFiles    int `json:"max_files,omitempty"`
}

// RepoMapResponse carries the formatted outline text plus accounting metadata.
type RepoMapResponse struct {
	Text    string `json:"text"`
	Files   int    `json:"files"`
	Skipped int    `json:"skipped"`
	Bytes   int    `json:"bytes"`
}

// RepoMap builds a tree-sitter outline of the workspace honouring the runner's
// configured exclude_dirs. Default budget is 8192 bytes — small enough to
// inject into the prompt without blowing the context, large enough to outline
// a medium-sized repo at top-level.
func (r *Runner) RepoMap(ctx context.Context, req RepoMapRequest) (*RepoMapResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	budget := req.BudgetBytes
	if budget == 0 {
		budget = 8192
	}
	rm, err := repomap.Build(ctx, r.workspaceRoot, repomap.Options{
		ExcludeDirs: r.excludeDirs,
		MaxFiles:    req.MaxFiles,
	})
	if err != nil {
		return nil, err
	}
	text := repomap.Format(rm, budget)
	return &RepoMapResponse{
		Text:    text,
		Files:   len(rm.Files),
		Skipped: rm.Skipped,
		Bytes:   len(text),
	}, nil
}

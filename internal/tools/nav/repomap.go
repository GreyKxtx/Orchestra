package nav

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/repomap"
)

type RepoMapRequest struct {
	BudgetBytes int `json:"budget_bytes,omitempty"`
	MaxFiles    int `json:"max_files,omitempty"`
}

type RepoMapResponse struct {
	Text    string `json:"text"`
	Files   int    `json:"files"`
	Skipped int    `json:"skipped"`
	Bytes   int    `json:"bytes"`
}

func (c *Client) RepoMap(ctx context.Context, req RepoMapRequest) (*RepoMapResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("nav client is nil")
	}
	budget := req.BudgetBytes
	if budget == 0 {
		budget = 8192
	}
	rm, err := repomap.Build(ctx, c.Root, repomap.Options{
		ExcludeDirs: c.ExcludeDirs,
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

package git

import (
	"context"
	"fmt"
	"strings"

	coregit "github.com/orchestra/orchestra/internal/git"
	"github.com/orchestra/orchestra/protocol"
)

// ─── git.worktree.* ───────────────────────────────────────────────────────────

type GitWorktreeListRequest struct{}

type GitWorktreeListResponse struct {
	Worktrees []coregit.WorktreeEntry `json:"worktrees"`
}

func (c *Client) GitWorktreeList(ctx context.Context, _ GitWorktreeListRequest) (*GitWorktreeListResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	entries, err := coregit.ListWorktrees(c.root)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, "git worktree list failed",
			map[string]any{"error": err.Error()})
	}
	return &GitWorktreeListResponse{Worktrees: entries}, nil
}

type GitWorktreeAddRequest struct {
	Name    string `json:"name"`
	Branch  string `json:"branch,omitempty"`
	BaseRef string `json:"base_ref,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

type GitWorktreeAddResponse struct {
	Worktree coregit.WorktreeEntry `json:"worktree"`
}

func (c *Client) GitWorktreeAdd(ctx context.Context, req GitWorktreeAddRequest) (*GitWorktreeAddResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "name is required", nil)
	}
	entry, err := coregit.AddWorktree(c.root, name, req.Branch, req.BaseRef, req.Force)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"name": name})
	}
	return &GitWorktreeAddResponse{Worktree: *entry}, nil
}

type GitWorktreeRemoveRequest struct {
	Name  string `json:"name"`
	Force bool   `json:"force,omitempty"`
}

type GitWorktreeRemoveResponse struct {
	Removed string `json:"removed"`
}

func (c *Client) GitWorktreeRemove(ctx context.Context, req GitWorktreeRemoveRequest) (*GitWorktreeRemoveResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "name is required", nil)
	}
	if err := coregit.RemoveWorktree(c.root, name, req.Force); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"name": name})
	}
	return &GitWorktreeRemoveResponse{Removed: name}, nil
}

type GitWorktreePruneRequest struct{}

type GitWorktreePruneResponse struct {
	RegistryRemoved int `json:"registry_removed"`
}

func (c *Client) GitWorktreePrune(ctx context.Context, _ GitWorktreePruneRequest) (*GitWorktreePruneResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	n, err := coregit.PruneWorktrees(c.root)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), nil)
	}
	return &GitWorktreePruneResponse{RegistryRemoved: n}, nil
}

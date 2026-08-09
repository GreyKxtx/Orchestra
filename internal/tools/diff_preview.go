package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)

// FSPreviewRequest is the input for the diff.preview tool.
type FSPreviewRequest struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

// FSPreviewResponse is the output of the diff.preview tool.
type FSPreviewResponse struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// FSPreview applies search→replace in memory and returns a unified diff without writing to disk.
// In dry-run mode it reads from the staging overlay first.
func (r *Runner) FSPreview(ctx context.Context, req FSPreviewRequest) (*FSPreviewResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	_ = ctx // no async ops; accepted for API consistency with other tools

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if req.Search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}

	absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
	if err != nil {
		return nil, err
	}

	var current []byte
	if r.dryRun {
		if staged, _, ok := r.stagedContent(relSlash); ok {
			current = []byte(staged)
		} else {
			b, readErr := os.ReadFile(absPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nil, readErr
			}
			current = b
		}
	} else {
		b, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return nil, fmt.Errorf("read file: %w", readErr)
		}
		current = b
	}

	newContent, applyErr := resolver.ApplySearchReplace(current, req.Search, req.Replace)
	if applyErr != nil {
		return nil, applyErr
	}

	diff := udiff.Unified("a/"+relSlash, "b/"+relSlash, string(current), string(newContent))
	return &FSPreviewResponse{Path: relSlash, Diff: diff}, nil
}

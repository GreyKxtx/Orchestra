package fs

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/patch/resolver"
)

func (c *Client) Preview(ctx context.Context, req FSPreviewRequest) (*FSPreviewResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	_ = ctx

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if req.Search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}

	absPath, relSlash, err := resolveWorkspacePath(c.Root, path)
	if err != nil {
		return nil, err
	}

	var current []byte
	if c.isDryRun() && c.Overlay != nil {
		if staged, _, ok := c.Overlay.stagedContent(relSlash); ok {
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

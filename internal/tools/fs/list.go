package fs

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) List(ctx context.Context, req FSListRequest) (*FSListResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	_ = ctx

	exclude := c.ExcludeDirs
	if len(req.ExcludeDirs) > 0 {
		exclude = req.ExcludeDirs
	}
	skipBackups := true
	if req.SkipBackups != nil {
		skipBackups = *req.SkipBackups
	}

	listPath := strings.TrimSpace(req.Path)
	if listPath == "" {
		listPath = "."
	}
	startAbs, relSlash, err := resolveWorkspacePath(c.Root, listPath)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(startAbs)
	if err != nil {
		if missing := missingPathError(c.Root, relSlash, err); missing != nil {
			return nil, missing
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			relSlash+" is a file, not a directory — use read to see its contents",
			map[string]any{"path": relSlash})
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	limit := req.Limit
	if req.MaxEntries > 0 {
		limit = req.MaxEntries
	}

	files, err := listFiles(c.Root, startAbs, exclude, skipBackups, req.IncludeHash, recursive, limit)
	if err != nil {
		return nil, err
	}
	if c.isDryRun() && c.Overlay != nil {
		files = c.Overlay.mergeStagedFilesIntoList(files, listPath, req.IncludeHash, limit)
	}
	return &FSListResponse{Files: files}, nil
}

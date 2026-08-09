package fs

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) Glob(ctx context.Context, req FSGlobRequest) (*FSGlobResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}

	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "pattern is empty", nil)
	}
	pattern = filepath.ToSlash(pattern)

	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") {
		return nil, protocol.NewError(protocol.PathTraversal, "pattern must be relative", map[string]any{"pattern": pattern})
	}
	for _, seg := range strings.Split(pattern, "/") {
		if seg == ".." {
			return nil, protocol.NewError(protocol.PathTraversal, "pattern must not contain ..", map[string]any{"pattern": pattern})
		}
	}

	exclude := c.ExcludeDirs
	if len(req.ExcludeDirs) > 0 {
		exclude = req.ExcludeDirs
	}
	excludeMap := make(map[string]bool, len(exclude))
	for _, d := range exclude {
		d = strings.TrimSpace(d)
		if d != "" {
			excludeMap[strings.Trim(d, "/\\")] = true
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 1000
	}

	var files []FSFileMeta

	walkErr := filepath.WalkDir(c.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, relErr := filepath.Rel(c.Root, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			if relSlash == "." {
				return nil
			}
			if excludeMap[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasSuffix(path, ".orchestra.bak") {
			return nil
		}

		if !matchGlobPath(pattern, relSlash) {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		meta := FSFileMeta{
			Path:  relSlash,
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
		}

		if req.IncludeHash {
			if h, hashErr := sha256File(path); hashErr == nil {
				meta.FileHash = h
			}
		}

		files = append(files, meta)
		if len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})

	if walkErr != nil {
		return nil, walkErr
	}

	if c.isDryRun() && c.Overlay != nil {
		files = c.Overlay.mergeStagedFilesIntoGlob(files, pattern, req.IncludeHash, limit)
	}

	return &FSGlobResponse{Files: files, Pattern: pattern}, nil
}

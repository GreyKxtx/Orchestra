package tools

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)

type FSGlobRequest struct {
	Pattern     string   `json:"pattern"`
	Limit       int      `json:"limit,omitempty"`
	IncludeHash bool     `json:"include_hash,omitempty"`
	ExcludeDirs []string `json:"exclude_dirs,omitempty"`
}

type FSGlobResponse struct {
	Files   []FSFileMeta `json:"files"`
	Pattern string       `json:"pattern"`
}

func (r *Runner) FSGlob(ctx context.Context, req FSGlobRequest) (*FSGlobResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
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

	exclude := r.excludeDirs
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

	walkErr := filepath.WalkDir(r.workspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, relErr := filepath.Rel(r.workspaceRoot, path)
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

	return &FSGlobResponse{Files: files, Pattern: pattern}, nil
}

// matchGlobPath reports whether relSlash matches pattern.
// Both use forward slashes. Pattern may use * and ? per filepath.Match, plus
// ** to match any number of path segments (including zero).
func matchGlobPath(pattern, relSlash string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(relSlash, "/"))
}

func matchSegments(pp, fp []string) bool {
	for len(pp) > 0 {
		seg := pp[0]
		pp = pp[1:]
		if seg == "**" {
			// ** matches zero or more path components.
			for i := 0; i <= len(fp); i++ {
				if matchSegments(pp, fp[i:]) {
					return true
				}
			}
			return false
		}
		if len(fp) == 0 {
			return false
		}
		matched, err := filepath.Match(seg, fp[0])
		if err != nil || !matched {
			return false
		}
		fp = fp[1:]
	}
	return len(fp) == 0
}

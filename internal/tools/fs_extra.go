package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
)

// FSDeleteRequest is the input for the fs.delete tool.
type FSDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

// FSDeleteResponse is the output of the fs.delete tool.
type FSDeleteResponse struct {
	Path string `json:"path"`
}

// FSDelete removes a file or directory at the given workspace-relative path.
// Requires recursive=true to remove non-empty directories.
// In dry-run mode the operation is a no-op (returns success without touching disk).
func (r *Runner) FSDelete(ctx context.Context, req FSDeleteRequest) (*FSDeleteResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	p := strings.TrimSpace(req.Path)
	if p == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	abs, relSlash, err := resolveWorkspacePath(r.workspaceRoot, p)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path does not exist",
			map[string]any{"path": relSlash})
	}

	if r.dryRun {
		return &FSDeleteResponse{Path: relSlash}, nil
	}

	var removeErr error
	if req.Recursive {
		removeErr = os.RemoveAll(abs)
	} else {
		removeErr = os.Remove(abs)
	}
	if removeErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("delete failed: %s", removeErr),
			map[string]any{"path": relSlash, "recursive": req.Recursive})
	}

	return &FSDeleteResponse{Path: relSlash}, nil
}

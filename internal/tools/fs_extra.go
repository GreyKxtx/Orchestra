package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/protocol"
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

	// H7 in audit ledger: tell LSP servers the document is gone so they
	// drop stale didOpen state + cached diagnostics. Best-effort.
	if r.lspManager != nil {
		r.lspManager.DidClose(ctx, relSlash)
	}

	return &FSDeleteResponse{Path: relSlash}, nil
}

// FSRenameRequest is the input for the fs.rename tool.
type FSRenameRequest struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

// FSRenameResponse is the output of the fs.rename tool.
type FSRenameResponse struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path"`
}

// FSRename moves or renames a file or directory within the workspace.
// Parent directories of new_path are created automatically.
// In dry-run mode the operation is a no-op (returns success without touching disk).
func (r *Runner) FSRename(ctx context.Context, req FSRenameRequest) (*FSRenameResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	src := strings.TrimSpace(req.Path)
	dst := strings.TrimSpace(req.NewPath)
	if src == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if dst == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "new_path is empty", nil)
	}

	absSrc, relSrc, err := resolveWorkspacePath(r.workspaceRoot, src)
	if err != nil {
		return nil, err
	}
	absDst, relDst, err := resolveWorkspacePath(r.workspaceRoot, dst)
	if err != nil {
		return nil, err
	}

	if absSrc == absDst {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "source and destination paths are identical",
			map[string]any{"path": relSrc})
	}

	if _, statErr := os.Stat(absSrc); os.IsNotExist(statErr) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "source path does not exist",
			map[string]any{"path": relSrc})
	}

	if _, statErr := os.Stat(absDst); statErr == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "destination path already exists",
			map[string]any{"new_path": relDst})
	}

	if r.dryRun {
		return &FSRenameResponse{Path: relSrc, NewPath: relDst}, nil
	}

	if mkErr := os.MkdirAll(filepath.Dir(absDst), 0o755); mkErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed, "failed to create parent directories",
			map[string]any{"new_path": relDst, "error": mkErr.Error()})
	}

	if renameErr := os.Rename(absSrc, absDst); renameErr != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("rename failed: %s", renameErr),
			map[string]any{"path": relSrc, "new_path": relDst})
	}

	// H7 in audit ledger: close the old URI on every server that had it
	// open; the next lsp.* tool that touches the new path will didOpen
	// it lazily via ensureOpen.
	if r.lspManager != nil {
		r.lspManager.DidClose(ctx, relSrc)
	}

	return &FSRenameResponse{Path: relSrc, NewPath: relDst}, nil
}

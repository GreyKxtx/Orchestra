package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) Delete(ctx context.Context, req FSDeleteRequest) (*FSDeleteResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	p := strings.TrimSpace(req.Path)
	if p == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	abs, relSlash, err := resolveWorkspacePath(c.Root, p)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path does not exist",
			map[string]any{"path": relSlash})
	}

	if c.isDryRun() {
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

	if c.Hooks.OnDidClose != nil {
		c.Hooks.OnDidClose(ctx, relSlash)
	}

	return &FSDeleteResponse{Path: relSlash}, nil
}

func (c *Client) Rename(ctx context.Context, req FSRenameRequest) (*FSRenameResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	src := strings.TrimSpace(req.Path)
	dst := strings.TrimSpace(req.NewPath)
	if src == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if dst == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "new_path is empty", nil)
	}

	absSrc, relSrc, err := resolveWorkspacePath(c.Root, src)
	if err != nil {
		return nil, err
	}
	absDst, relDst, err := resolveWorkspacePath(c.Root, dst)
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

	if c.isDryRun() {
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

	if c.Hooks.OnDidClose != nil {
		c.Hooks.OnDidClose(ctx, relSrc)
	}

	return &FSRenameResponse{Path: relSrc, NewPath: relDst}, nil
}

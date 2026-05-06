package tools

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)

type FSEditRequest struct {
	Path     string `json:"path"`
	Search   string `json:"search"`
	Replace  string `json:"replace"`
	FileHash string `json:"file_hash,omitempty"` // strongly recommended; mismatch → StaleContent
	Backup   bool   `json:"backup,omitempty"`
}

type FSEditResponse struct {
	Path     string `json:"path"`
	FileHash string `json:"file_hash"` // sha256 of file after edit
}

func (r *Runner) FSEdit(ctx context.Context, req FSEditRequest) (*FSEditResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if req.Search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}

	patch := patches.Patch{
		Type:     patches.TypeFileSearchReplace,
		Path:     path,
		Search:   req.Search,
		Replace:  req.Replace,
		FileHash: strings.TrimSpace(req.FileHash),
	}

	opsList, err := resolver.ResolveExternalPatches(r.workspaceRoot, []patches.Patch{patch})
	if err != nil {
		return nil, err
	}

	_, err = applier.ApplyAnyOps(r.workspaceRoot, opsList, applier.ApplyOptions{
		DryRun:       false,
		Backup:       req.Backup,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		return nil, err
	}

	// Read new hash from the written file.
	absPath, relSlash, resolveErr := resolveWorkspacePath(r.workspaceRoot, path)
	if resolveErr != nil {
		return nil, resolveErr
	}
	_, _, _, newHash, _, readErr := readFileWithHash(absPath, -1)
	if readErr != nil {
		// Apply succeeded — return path without hash rather than failing.
		return &FSEditResponse{Path: relSlash}, nil
	}
	return &FSEditResponse{Path: relSlash, FileHash: newHash}, nil
}

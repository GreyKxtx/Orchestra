package tools

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
)

type FSEditRequest struct {
	Path         string `json:"path"`
	Search       string `json:"search"`
	Replace      string `json:"replace"`
	FileHash     string `json:"file_hash,omitempty"` // strongly recommended; mismatch → StaleContent
	TargetSymbol string `json:"target_symbol,omitempty"`
	Backup       bool   `json:"backup,omitempty"`
}

type FSEditResponse struct {
	Path        string               `json:"path"`
	FileHash    string               `json:"file_hash"` // sha256 of file after edit
	Diagnostics []lsp.ToolDiagnostic `json:"diagnostics,omitempty"`
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

	// Dry-run: apply search/replace against overlay or disk, stage result.
	if r.dryRun {
		absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
		if err != nil {
			return nil, err
		}
		var currentContent []byte
		if staged, _, ok := r.stagedContent(relSlash); ok {
			currentContent = []byte(staged)
		} else {
			b, readErr := os.ReadFile(absPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nil, readErr
			}
			currentContent = b
		}
		newContent, err := r.applyEditSearchReplace(ctx, relSlash, currentContent, req.Search, req.Replace, req.TargetSymbol)
		if err != nil {
			return nil, err
		}
		newHash := cache.ComputeSHA256(newContent)
		if err := r.stageFile(relSlash, string(newContent), newHash); err != nil {
			return nil, err
		}
		var diags []lsp.ToolDiagnostic
		if r.lspManager != nil && !r.lspManager.IsEmpty() {
			diags = r.lspManager.SyncAndDiagnose(ctx, relSlash, string(newContent))
		}
		diags = append(diags, r.extraTestDiagnostics(string(newContent))...)
		return &FSEditResponse{Path: relSlash, FileHash: newHash, Diagnostics: diags}, nil
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

	// Read new content and hash from the written file.
	absPath, relSlash, resolveErr := resolveWorkspacePath(r.workspaceRoot, path)
	if resolveErr != nil {
		return nil, resolveErr
	}
	content, _, _, newHash, _, readErr := readFileWithHash(absPath, -1)
	if readErr != nil {
		// Apply succeeded — return path without hash rather than failing.
		return &FSEditResponse{Path: relSlash}, nil
	}

	var diags []lsp.ToolDiagnostic
	if r.lspManager != nil && !r.lspManager.IsEmpty() {
		diags = r.lspManager.SyncAndDiagnose(ctx, relSlash, content)
	}
	diags = append(diags, r.extraTestDiagnostics(string(content))...)

	return &FSEditResponse{Path: relSlash, FileHash: newHash, Diagnostics: diags}, nil
}

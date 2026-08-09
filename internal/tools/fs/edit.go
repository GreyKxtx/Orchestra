package fs

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/patch/resolver"
)

func (c *Client) Edit(ctx context.Context, req FSEditRequest) (*FSEditResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	if req.Search == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", nil)
	}

	if c.isDryRun() && c.Overlay != nil {
		absPath, relSlash, err := resolveWorkspacePath(c.Root, path)
		if err != nil {
			return nil, err
		}
		var currentContent []byte
		if staged, _, ok := c.Overlay.stagedContent(relSlash); ok {
			currentContent = []byte(staged)
		} else {
			b, readErr := os.ReadFile(absPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return nil, readErr
			}
			currentContent = b
		}
		newContent, err := c.applyEditSearchReplace(ctx, relSlash, currentContent, req.Search, req.Replace, req.TargetSymbol)
		if err != nil {
			return nil, err
		}
		newHash := cache.ComputeSHA256(newContent)
		if err := c.Overlay.stageFile(c, relSlash, string(newContent), newHash); err != nil {
			return nil, err
		}
		var diags []ToolDiagnostic
		pending := false
		if c.Hooks.Diagnose != nil {
			diags, pending = c.Hooks.Diagnose(ctx, relSlash, string(newContent))
		}
		diags = append(diags, c.extraDiagnostics(string(newContent))...)
		return &FSEditResponse{Path: relSlash, FileHash: newHash, Diagnostics: diags, DiagnosticsPending: pending}, nil
	}

	patch := patches.Patch{
		Type:     patches.TypeFileSearchReplace,
		Path:     path,
		Search:   req.Search,
		Replace:  req.Replace,
		FileHash: strings.TrimSpace(req.FileHash),
	}

	opsList, err := resolver.ResolveExternalPatches(c.Root, []patches.Patch{patch})
	if err != nil {
		return nil, err
	}

	_, err = applier.ApplyAnyOps(c.Root, opsList, applier.ApplyOptions{
		DryRun:       false,
		Backup:       req.Backup,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		return nil, err
	}

	absPath, relSlash, resolveErr := resolveWorkspacePath(c.Root, path)
	if resolveErr != nil {
		return nil, resolveErr
	}
	content, _, _, newHash, _, readErr := readFileWithHash(absPath, -1)
	if readErr != nil {
		return &FSEditResponse{Path: relSlash}, nil
	}

	var diags []ToolDiagnostic
	pending := false
	if c.Hooks.Diagnose != nil {
		diags, pending = c.Hooks.Diagnose(ctx, relSlash, content)
	}
	diags = append(diags, c.extraDiagnostics(content)...)

	return &FSEditResponse{Path: relSlash, FileHash: newHash, Diagnostics: diags, DiagnosticsPending: pending}, nil
}

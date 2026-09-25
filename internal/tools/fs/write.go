package fs

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/patch/resolver"
	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) Write(ctx context.Context, req FSWriteRequest) (*FSWriteResponse, error) {
	c = c.at(ctx)
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	fileHash := strings.TrimSpace(req.FileHash)
	_, relSlash, pathErr := resolveWorkspacePath(c.Root, path)
	if pathErr != nil {
		return nil, pathErr
	}
	if !req.MustNotExist && fileHash == "" && c.Overlay != nil && !c.Overlay.fileExistsOnDisk(relSlash) {
		if _, _, ok := c.Overlay.stagedContent(relSlash); !ok {
			req.MustNotExist = true
		}
	}
	if !req.MustNotExist && fileHash == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			"fs.write requires file_hash (for overwrite) or must_not_exist=true (for create)", nil)
	}
	// A file_hash for a file that does not exist is a create dressed as an
	// overwrite — the model has just read some other file and pasted its hash,
	// as the tool description told it to for overwrites. Answering "file hash
	// mismatch" is true and useless: the 27B repeated the same write five
	// times on plan_orders_two_files (2026-09-18) and never learned that the
	// file was simply not there. Say so, and say what to send instead.
	if !req.MustNotExist && fileHash != "" && !c.fileKnown(relSlash) {
		return nil, protocol.NewError(protocol.NotFound,
			"fs.write: "+relSlash+" does not exist, so there is no version for file_hash to match — "+
				"to create it pass must_not_exist=true and no file_hash; file_hash is for overwriting a file you have read",
			map[string]any{"path": relSlash})
	}

	if c.isDryRun() && c.Overlay != nil {
		if req.MustNotExist && c.Overlay.fileExistsOnDisk(relSlash) {
			return nil, protocol.NewError(protocol.AlreadyExists, "file already exists", map[string]any{"path": relSlash})
		}
		if fileHash != "" {
			if current := c.Overlay.currentHash(relSlash); current != fileHash {
				return nil, protocol.NewError(protocol.StaleContent, "file hash mismatch", map[string]any{
					"path":     relSlash,
					"expected": fileHash,
					"actual":   current,
				})
			}
		}
		// Staging bypasses patch resolution, so the destructive-write guard
		// has to be asked here too — otherwise a dry run stages the file
		// emptied and the model sees its own damage confirmed.
		previousContent := ""
		if current, ok := c.Overlay.currentContent(c, relSlash); ok {
			previousContent = current
		}
		if !req.MustNotExist && previousContent != "" {
			if reason := resolver.DestructiveWriteReason(previousContent, req.Content); reason != "" {
				return nil, protocol.NewError(protocol.InvalidLLMOutput,
					"refusing to write "+relSlash+": "+reason,
					map[string]any{"path": relSlash})
			}
		}

		contentHash := cache.ComputeSHA256([]byte(req.Content))
		if err := c.Overlay.stageFile(c, relSlash, req.Content, contentHash); err != nil {
			return nil, err
		}
		var diags []ToolDiagnostic
		pending := false
		if c.Hooks.Diagnose != nil {
			diags, pending = c.Hooks.Diagnose(ctx, relSlash, req.Content)
		}
		diags = append(diags, c.extraDiagnostics(req.Content)...)
		applied, region := describeChange(relSlash, previousContent, req.Content)
		return &FSWriteResponse{
			Path:               relSlash,
			FileHash:           contentHash,
			BytesWritten:       len(req.Content),
			Applied:            applied,
			ChangedRegion:      region,
			Diagnostics:        diags,
			DiagnosticsPending: pending,
		}, nil
	}

	if err := c.gateSyntax(relSlash, req.Content); err != nil {
		return nil, err
	}

	patch := patches.Patch{
		Type:    patches.TypeFileWriteAtomic,
		Path:    path,
		Content: req.Content,
	}
	if req.MustNotExist {
		patch.Conditions = &patches.WriteAtomicConditions{MustNotExist: true}
	} else {
		patch.Conditions = &patches.WriteAtomicConditions{FileHash: fileHash}
	}

	opsList, err := resolver.ResolveExternalPatches(c.Root, []patches.Patch{patch})
	if err != nil {
		return nil, err
	}

	// Read before applying: afterwards the previous content is gone, and
	// without it the tool cannot tell the model what its write actually did.
	previousContent := ""
	if absPath, _, e := resolveWorkspacePath(c.Root, path); e == nil {
		if b, readErr := os.ReadFile(absPath); readErr == nil {
			previousContent = string(b)
		}
	}

	_, err = applier.ApplyAnyOps(c.Root, opsList, applier.ApplyOptions{
		DryRun:       false,
		Backup:       req.Backup,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		return nil, err
	}

	contentHash := cache.ComputeSHA256([]byte(req.Content))

	var diags []ToolDiagnostic
	pending := false
	if _, relSlash, err := resolveWorkspacePath(c.Root, path); err == nil && c.Hooks.Diagnose != nil {
		diags, pending = c.Hooks.Diagnose(ctx, relSlash, req.Content)
	}
	diags = append(diags, c.extraDiagnostics(req.Content)...)

	applied, region := describeChange(relSlash, previousContent, req.Content)

	return &FSWriteResponse{
		Path:               path,
		FileHash:           contentHash,
		BytesWritten:       len(req.Content),
		Applied:            applied,
		ChangedRegion:      region,
		Diagnostics:        diags,
		DiagnosticsPending: pending,
	}, nil
}

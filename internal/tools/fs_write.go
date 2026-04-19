package tools

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/applier"
	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
	"github.com/orchestra/orchestra/internal/store"
)

type FSWriteRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	FileHash     string `json:"file_hash,omitempty"`     // verify before overwriting
	MustNotExist bool   `json:"must_not_exist,omitempty"` // fail if file already exists
	Backup       bool   `json:"backup,omitempty"`
}

type FSWriteResponse struct {
	Path         string `json:"path"`
	FileHash     string `json:"file_hash"` // sha256 of written content
	BytesWritten int    `json:"bytes_written"`
}

func (r *Runner) FSWrite(ctx context.Context, req FSWriteRequest) (*FSWriteResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	fileHash := strings.TrimSpace(req.FileHash)
	if !req.MustNotExist && fileHash == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			"fs.write requires file_hash (for overwrite) or must_not_exist=true (for create)", nil)
	}

	patch := externalpatch.Patch{
		Type:    externalpatch.TypeFileWriteAtomic,
		Path:    path,
		Content: req.Content,
	}
	if req.MustNotExist {
		patch.Conditions = &externalpatch.WriteAtomicConditions{MustNotExist: true}
	} else {
		patch.Conditions = &externalpatch.WriteAtomicConditions{FileHash: fileHash}
	}

	opsList, err := resolver.ResolveExternalPatches(r.workspaceRoot, []externalpatch.Patch{patch})
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

	contentHash := store.ComputeSHA256([]byte(req.Content))
	return &FSWriteResponse{
		Path:         path,
		FileHash:     contentHash,
		BytesWritten: len(req.Content),
	}, nil
}

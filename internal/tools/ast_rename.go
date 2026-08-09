package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/orchestra/orchestra/internal/astedit"
	"github.com/orchestra/orchestra/patch/cache"
)

// ASTRenameRequest is the JSON input for ast_rename.
type ASTRenameRequest struct {
	Path    string `json:"path"`
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

// ASTRenameResponse summarises one ast_rename call. When Count == 0 the file
// is left untouched and Wrote is false.
type ASTRenameResponse struct {
	Path         string `json:"path"`
	Count        int    `json:"count"`
	Sites        []int  `json:"sites,omitempty"`
	SkippedSites []int  `json:"skipped_sites,omitempty"`
	Wrote        bool   `json:"wrote"`
	NewHash      string `json:"new_file_hash,omitempty"`
}

// ASTRename performs an identifier-aware rename in a single file. Hits inside
// strings and comments are deliberately skipped. The write goes through FSWrite
// so it inherits the same staging-overlay / dry-run / file-hash safety rails
// as every other mutating fs tool.
func (r *Runner) ASTRename(ctx context.Context, req ASTRenameRequest) (*ASTRenameResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if req.Path == "" {
		return nil, fmt.Errorf("ast_rename: path is empty")
	}

	abs, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("ast_rename: read %s: %w", abs, err)
	}
	prevHash := cache.ComputeSHA256(src)

	res, err := astedit.RenameInFile(ctx, abs, src, req.OldName, req.NewName)
	if err != nil {
		return nil, err
	}
	resp := &ASTRenameResponse{
		Path:         relSlash,
		Count:        res.Count,
		Sites:        res.Sites,
		SkippedSites: res.Skipped,
	}
	if res.Count == 0 {
		return resp, nil
	}

	// Route through FSWrite so the change picks up staging overlay in dry-run
	// mode and atomic+hash-verified write in apply mode. file_hash is the SHA
	// of the version we just read — the resolver re-checks before mutating.
	writeResp, werr := r.FSWrite(ctx, FSWriteRequest{
		Path:     relSlash,
		Content:  string(res.NewContent),
		FileHash: prevHash,
	})
	if werr != nil {
		return nil, werr
	}
	resp.Wrote = true
	resp.NewHash = writeResp.FileHash
	return resp, nil
}

package fs

import (
	"context"
	"fmt"
	"os"

	"github.com/orchestra/orchestra/internal/astedit"
	"github.com/orchestra/orchestra/patch/cache"
)

func (c *Client) ASTRename(ctx context.Context, req ASTRenameRequest) (*ASTRenameResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	if req.Path == "" {
		return nil, fmt.Errorf("ast_rename: path is empty")
	}

	abs, relSlash, err := resolveWorkspacePath(c.Root, req.Path)
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

	writeResp, werr := c.Write(ctx, FSWriteRequest{
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

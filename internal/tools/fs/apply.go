package fs

import (
	"context"

	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/protocol"
)

func (c *Client) ApplyOps(ctx context.Context, req FSApplyOpsRequest) (*FSApplyOpsResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "client is nil", nil)
	}
	_ = ctx

	res, err := applier.ApplyAnyOps(c.Root, req.Ops, applier.ApplyOptions{
		DryRun:       req.DryRun,
		Backup:       req.Backup,
		BackupSuffix: ".orchestra.bak",
	})
	if err != nil {
		return nil, err
	}
	return &FSApplyOpsResponse{
		Diffs:        res.Diffs,
		ChangedFiles: res.ChangedFiles,
		Applied:      !req.DryRun,
	}, nil
}

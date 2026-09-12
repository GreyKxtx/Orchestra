package nav

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/ckg"
)

// CKGGraph is the code knowledge graph as the UI draws it: at "file" level
// folders, files and weighted file-to-file relations; at "symbol" level every
// indexed symbol. ok is false when there is no graph store for the workspace,
// which is an answer, not an error — the caller says "nothing indexed yet".
func (c *Client) CKGGraph(ctx context.Context, level string) (*ckg.GraphData, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return nil, false, nil
	}
	switch level {
	case "symbol":
		g, err := ckg.BuildGraphData(ctx, snap.Store)
		return g, true, err
	case "", "file":
		g, err := ckg.BuildFileGraphData(ctx, snap.Store)
		return g, true, err
	default:
		return nil, true, fmt.Errorf("unknown graph level %q (want file or symbol)", level)
	}
}

// CKGFileOutline is one file's indexed symbols; ok is false when there is no
// graph store for the workspace, the same answer CKGGraph gives.
func (c *Client) CKGFileOutline(ctx context.Context, path string) (*ckg.FileOutline, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return nil, false, nil
	}
	o, err := ckg.BuildFileOutline(ctx, snap.Store, path)
	return o, true, err
}

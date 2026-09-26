package nav

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lsp"
)

// CKGAccess is a consistent snapshot of the CKG store and provider.
type CKGAccess struct {
	Store    *ckg.Store
	Provider *ckg.Provider
}

// Client executes navigation, CKG, and repo-map tools.
type Client struct {
	Root        string
	ExcludeDirs []string
	EmbedCfg    config.EmbedConfig
	snapshot    func() (CKGAccess, func())
	lsp         func() *lsp.Manager
	staged      func(ctx context.Context) ([]ckg.StagedFile, func(string) ([]byte, bool))
}

// NewClient wires navigation tools. snapshot must return a read-locked CKG view.
func NewClient(
	root string,
	exclude []string,
	embedCfg config.EmbedConfig,
	snapshot func() (CKGAccess, func()),
	lspFn func() *lsp.Manager,
) *Client {
	return &Client{
		Root:        root,
		ExcludeDirs: append([]string(nil), exclude...),
		EmbedCfg:    embedCfg,
		snapshot:    snapshot,
		lsp:         lspFn,
	}
}

func (c *Client) withCKG(fn func(CKGAccess) error) error {
	if c == nil || c.snapshot == nil {
		return fmt.Errorf("ckg unavailable")
	}
	snap, unlock := c.snapshot()
	defer unlock()
	return fn(snap)
}

func (c *Client) ckgSnap() (CKGAccess, func()) {
	if c == nil || c.snapshot == nil {
		return CKGAccess{}, func() {}
	}
	return c.snapshot()
}

func (c *Client) lspManager() *lsp.Manager {
	if c == nil || c.lsp == nil {
		return nil
	}
	return c.lsp()
}

// WithStaged tells the client where a turn's staged files are: explore and
// the file outline answer from the graph with those files in place of the
// disk's (LLM-11).
func (c *Client) WithStaged(fn func(ctx context.Context) ([]ckg.StagedFile, func(string) ([]byte, bool))) *Client {
	if c != nil {
		c.staged = fn
	}
	return c
}

// overlayCtx is ctx with the graph shadowed by the staged files of the
// agent behind it, and the release that ends that; ctx itself when nothing
// is staged.
func (c *Client) overlayCtx(ctx context.Context, orch *ckg.Orchestrator) (context.Context, func(), error) {
	if c == nil || c.staged == nil {
		return ctx, func() {}, nil
	}
	files, read := c.staged(ctx)
	if len(files) == 0 {
		return ctx, func() {}, nil
	}
	return orch.OverlayContext(ctx, files, read)
}

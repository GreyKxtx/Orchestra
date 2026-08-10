package nav

import (
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

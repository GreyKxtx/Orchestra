package fs

import (
	"context"
)

// Hooks wires optional Runner integrations (LSP, CKG, memory) without
// importing the parent tools package.
type Hooks struct {
	OnStageSync          func(relSlash, content string)
	Diagnose             func(ctx context.Context, relSlash, content string) (diags []ToolDiagnostic, pending bool)
	ExtraDiagnostics     func(content string) []ToolDiagnostic
	GoFileRedirect       func(ctx context.Context, relSlash, hash string) string
	DiscoverInstructions   func(absDir string) string
	SymbolLineRange      func(ctx context.Context, relPath, symbol string) (start, end int, ok bool)
	SymbolFQNAtLine      func(ctx context.Context, relPath string, line int) string
	OnDidClose           func(ctx context.Context, relSlash string)
}

// Client executes filesystem tools inside a workspace root.
type Client struct {
	Root        string
	ExcludeDirs []string
	Overlay     *Overlay
	Hooks       Hooks
}

// NewClient returns a filesystem tool client. overlay may be nil (no staging).
func NewClient(root string, exclude []string, overlay *Overlay) *Client {
	return &Client{
		Root:        root,
		ExcludeDirs: exclude,
		Overlay:     overlay,
	}
}

func (c *Client) isDryRun() bool {
	if c == nil || c.Overlay == nil {
		return false
	}
	return c.Overlay.DryRun
}

func (c *Client) extraDiagnostics(content string) []ToolDiagnostic {
	if c == nil || c.Hooks.ExtraDiagnostics == nil {
		return nil
	}
	return c.Hooks.ExtraDiagnostics(content)
}

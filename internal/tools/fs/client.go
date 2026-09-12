package fs

import (
	"context"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/ops"
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

// gateSyntax rejects content that does not parse, when the AST gate is on.
//
// Overlay.stageFile has always done this, which covered the dry-run path and
// only that one: an apply went straight to the applier and was never asked.
// The check therefore ran exactly where nothing could be damaged and stayed
// silent where something could — a model rewrote a Go file without its
// package clause and the workspace stopped compiling.
//
// ValidateSyntax is conservative by construction: no grammar for the
// extension, empty content or an unparseable tree all return nil, so only a
// real ERROR/MISSING node refuses the write.
func (c *Client) gateSyntax(relSlash, content string) error {
	if c == nil || c.Overlay == nil || !c.Overlay.ASTGate {
		return nil
	}
	return ckg.ValidateSyntax(relSlash, []byte(content))
}

// gateEditResult runs the ops through the applier without writing anything and
// checks the content each file would be left with.
func (c *Client) gateEditResult(opsList []ops.AnyOp) error {
	if c == nil || c.Overlay == nil || !c.Overlay.ASTGate || len(opsList) == 0 {
		return nil
	}
	preview, err := applier.ApplyAnyOps(c.Root, opsList, applier.ApplyOptions{DryRun: true})
	if err != nil {
		// The real apply below will surface this properly; the gate is not
		// the place to report a resolution failure.
		return nil
	}
	for _, d := range preview.Diffs {
		if err := ckg.ValidateSyntax(d.Path, []byte(d.After)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) extraDiagnostics(content string) []ToolDiagnostic {
	if c == nil || c.Hooks.ExtraDiagnostics == nil {
		return nil
	}
	return c.Hooks.ExtraDiagnostics(content)
}

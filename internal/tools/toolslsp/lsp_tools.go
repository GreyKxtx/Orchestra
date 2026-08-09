package toolslsp

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools/toolpath"
)

// Client wraps LSP tool calls for a workspace.
type Client struct {
	root string
	manager *lsp.Manager
}

func NewClient(root string, mgr *lsp.Manager) *Client { return &Client{root: root, manager: mgr} }

// --- lsp.definition ---

type LSPDefinitionRequest struct {
	Path string `json:"path"`
	Line int    `json:"line"` // 1-based
	Col  int    `json:"col"`  // 1-based byte offset
}

type LSPDefinitionResponse struct {
	Locations []lsp.ToolLocation `json:"locations"`
}

func (c *Client) LSPDefinition(ctx context.Context, req LSPDefinitionRequest) (*LSPDefinitionResponse, error) {
	if c.manager == nil || c.manager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := toolpath.ResolveWorkspacePath(c.root, req.Path)
	if err != nil {
		return nil, err
	}
	locs, err := c.manager.Definition(ctx, relSlash, lsp.ToolPosition{Line: req.Line, Col: req.Col})
	if err != nil {
		return nil, err
	}
	return &LSPDefinitionResponse{Locations: locs}, nil
}

// --- lsp.references ---

type LSPReferencesRequest struct {
	Path               string `json:"path"`
	Line               int    `json:"line"`
	Col                int    `json:"col"`
	IncludeDeclaration bool   `json:"include_declaration,omitempty"`
}

type LSPReferencesResponse struct {
	Locations []lsp.ToolLocation `json:"locations"`
}

func (c *Client) LSPReferences(ctx context.Context, req LSPReferencesRequest) (*LSPReferencesResponse, error) {
	if c.manager == nil || c.manager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := toolpath.ResolveWorkspacePath(c.root, req.Path)
	if err != nil {
		return nil, err
	}
	locs, err := c.manager.References(ctx, relSlash,
		lsp.ToolPosition{Line: req.Line, Col: req.Col}, req.IncludeDeclaration)
	if err != nil {
		return nil, err
	}
	return &LSPReferencesResponse{Locations: locs}, nil
}

// --- lsp.hover ---

type LSPHoverRequest struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type LSPHoverResponse struct {
	Content string `json:"content"`
}

func (c *Client) LSPHover(ctx context.Context, req LSPHoverRequest) (*LSPHoverResponse, error) {
	if c.manager == nil || c.manager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := toolpath.ResolveWorkspacePath(c.root, req.Path)
	if err != nil {
		return nil, err
	}
	text, err := c.manager.Hover(ctx, relSlash, lsp.ToolPosition{Line: req.Line, Col: req.Col})
	if err != nil {
		return nil, err
	}
	if text == "" {
		return &LSPHoverResponse{Content: "(no hover information available)"}, nil
	}
	return &LSPHoverResponse{Content: text}, nil
}

// --- lsp.diagnostics ---

type LSPDiagnosticsRequest struct {
	Path string `json:"path"`
}

type LSPDiagnosticsResponse struct {
	Diagnostics []lsp.ToolDiagnostic `json:"diagnostics"`
}

func (c *Client) LSPDiagnostics(ctx context.Context, req LSPDiagnosticsRequest) (*LSPDiagnosticsResponse, error) {
	if c.manager == nil || c.manager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := toolpath.ResolveWorkspacePath(c.root, req.Path)
	if err != nil {
		return nil, err
	}
	diags, err := c.manager.GetDiagnostics(ctx, relSlash)
	if err != nil {
		return nil, err
	}
	return &LSPDiagnosticsResponse{Diagnostics: diags}, nil
}

// --- lsp.rename ---

type LSPRenameRequest struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	NewName string `json:"new_name"`
}

type LSPRenameResponse struct {
	Edits []lsp.ProposedEdit `json:"edits"`
}

func (c *Client) LSPRename(ctx context.Context, req LSPRenameRequest) (*LSPRenameResponse, error) {
	if c.manager == nil || c.manager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	if strings.TrimSpace(req.NewName) == "" {
		return nil, fmt.Errorf("lsp.rename: new_name is required")
	}
	_, relSlash, err := toolpath.ResolveWorkspacePath(c.root, req.Path)
	if err != nil {
		return nil, err
	}
	edits, err := c.manager.Rename(ctx, relSlash,
		lsp.ToolPosition{Line: req.Line, Col: req.Col}, req.NewName)
	if err != nil {
		return nil, err
	}
	return &LSPRenameResponse{Edits: edits}, nil
}


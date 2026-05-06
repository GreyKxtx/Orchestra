package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
)

// --- lsp.definition ---

type LSPDefinitionRequest struct {
	Path string `json:"path"`
	Line int    `json:"line"` // 1-based
	Col  int    `json:"col"`  // 1-based byte offset
}

type LSPDefinitionResponse struct {
	Locations []lsp.ToolLocation `json:"locations"`
}

func (r *Runner) LSPDefinition(ctx context.Context, req LSPDefinitionRequest) (*LSPDefinitionResponse, error) {
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}
	locs, err := r.lspManager.Definition(ctx, relSlash, lsp.ToolPosition{Line: req.Line, Col: req.Col})
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

func (r *Runner) LSPReferences(ctx context.Context, req LSPReferencesRequest) (*LSPReferencesResponse, error) {
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}
	locs, err := r.lspManager.References(ctx, relSlash,
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

func (r *Runner) LSPHover(ctx context.Context, req LSPHoverRequest) (*LSPHoverResponse, error) {
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}
	text, err := r.lspManager.Hover(ctx, relSlash, lsp.ToolPosition{Line: req.Line, Col: req.Col})
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

func (r *Runner) LSPDiagnostics(ctx context.Context, req LSPDiagnosticsRequest) (*LSPDiagnosticsResponse, error) {
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}
	diags, err := r.lspManager.GetDiagnostics(ctx, relSlash)
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

func (r *Runner) LSPRename(ctx context.Context, req LSPRenameRequest) (*LSPRenameResponse, error) {
	if r.lspManager == nil || r.lspManager.IsEmpty() {
		return nil, fmt.Errorf("lsp: no servers configured (add lsp.servers to .orchestra.yml)")
	}
	if strings.TrimSpace(req.NewName) == "" {
		return nil, fmt.Errorf("lsp.rename: new_name is required")
	}
	_, relSlash, err := resolveWorkspacePath(r.workspaceRoot, req.Path)
	if err != nil {
		return nil, err
	}
	edits, err := r.lspManager.Rename(ctx, relSlash,
		lsp.ToolPosition{Line: req.Line, Col: req.Col}, req.NewName)
	if err != nil {
		return nil, err
	}
	return &LSPRenameResponse{Edits: edits}, nil
}

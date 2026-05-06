package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
)

func (r *Runner) CodeSymbols(ctx context.Context, req CodeSymbolsRequest) (*CodeSymbolsResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	absPath, relSlash, err := resolveWorkspacePath(r.workspaceRoot, path)
	if err != nil {
		return nil, err
	}

	st, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("path is a directory")
	}

	// Tier 0: LSP documentSymbol — accurate, any language, no size limit.
	if r.lspManager != nil && !r.lspManager.IsEmpty() {
		if syms, err := r.lspManager.DocumentSymbols(ctx, relSlash); err == nil && len(syms) > 0 {
			return &CodeSymbolsResponse{Symbols: lspSymbolsToCodeSymbols(syms)}, nil
		}
	}

	// Tiers 1–3: Go only (regex/tree-sitter heuristics).
	if !strings.HasSuffix(strings.ToLower(relSlash), ".go") {
		return &CodeSymbolsResponse{Symbols: nil}, nil
	}
	if st.Size() > 2*1024*1024 {
		return &CodeSymbolsResponse{Symbols: nil}, nil
	}

	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	// Tier 1: Tree-sitter (if CGO available and parse succeeds).
	if syms, ok := goSymbolsViaTreeSitter(ctx, src); ok {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	// Tier 2: regex heuristics.
	if syms := goSymbolsViaRegex(src); len(syms) > 0 {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	// Tier 3: empty.
	_ = filepath.ToSlash // keep filepath imported when build tags change
	return &CodeSymbolsResponse{Symbols: nil}, nil
}

// lspSymbolsToCodeSymbols converts LSP ToolSymbol (1-based) to ops.Range-based Symbol (0-based).
func lspSymbolsToCodeSymbols(in []lsp.ToolSymbol) []Symbol {
	out := make([]Symbol, len(in))
	for i, s := range in {
		out[i] = Symbol{
			Name: s.Name,
			Kind: s.Kind,
			Range: &ops.Range{
				Start: ops.Position{Line: s.StartLine - 1, Col: s.StartCol - 1},
				End:   ops.Position{Line: s.EndLine - 1, Col: s.EndCol - 1},
			},
		}
	}
	return out
}

// Tier 2 heuristic: best-effort Go symbols using regex.
func goSymbolsViaRegex(src []byte) []Symbol {
	text := string(src)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	// func (r *T) Name(
	reFunc := regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	// type Name ...
	reType := regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

	var out []Symbol

	for i, line := range lines {
		if m := reFunc.FindStringSubmatchIndex(line); m != nil {
			name := line[m[2]:m[3]]
			col := m[2]
			out = append(out, Symbol{
				Name: name, Kind: "function",
				Range: &ops.Range{
					Start: ops.Position{Line: i, Col: col},
					End:   ops.Position{Line: i, Col: col + len(name)},
				},
			})
			continue
		}
		if m := reType.FindStringSubmatchIndex(line); m != nil {
			name := line[m[2]:m[3]]
			col := m[2]
			out = append(out, Symbol{
				Name: name, Kind: "type",
				Range: &ops.Range{
					Start: ops.Position{Line: i, Col: col},
					End:   ops.Position{Line: i, Col: col + len(name)},
				},
			})
		}
	}

	return out
}

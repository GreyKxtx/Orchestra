package nav

// CodeSymbols resolves file outlines via a three-tier fallback:
//  1. LSP document symbols (when gopls/other servers are configured)
//  2. tree-sitter Go parse (when CGO is enabled at build time)
//  3. line-based regex heuristics for Go (always available, no CGO)
//
// Non-Go files without LSP return an empty symbol list.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/tools/toolpath"
	"github.com/orchestra/orchestra/protocol"
)

type CodeSymbolsRequest struct {
	Path string `json:"path"`
}

// Symbol is one outline entry. Positions are 1-based, like read's line prefixes
// and the LSP tools' positions: the model never sees 0-based ops coordinates.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

type CodeSymbolsResponse struct {
	Symbols []Symbol `json:"symbols"`
}

func (c *Client) CodeSymbols(ctx context.Context, req CodeSymbolsRequest) (*CodeSymbolsResponse, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "nav client is nil", nil)
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	absPath, relSlash, err := toolpath.ResolveWorkspacePath(c.Root, path)
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

	mgr := c.lspManager()
	if mgr != nil && !mgr.IsEmpty() {
		if syms, err := mgr.DocumentSymbols(ctx, relSlash); err == nil && len(syms) > 0 {
			return &CodeSymbolsResponse{Symbols: lspSymbolsToCodeSymbols(syms)}, nil
		}
	}

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

	if syms, ok := goSymbolsViaTreeSitter(ctx, src); ok {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	if syms := goSymbolsViaRegex(src); len(syms) > 0 {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	return &CodeSymbolsResponse{Symbols: nil}, nil
}

func lspSymbolsToCodeSymbols(in []lsp.ToolSymbol) []Symbol {
	out := make([]Symbol, len(in))
	for i, s := range in {
		// lsp.ToolSymbol is already 1-based.
		out[i] = Symbol{
			Name: s.Name, Kind: s.Kind,
			StartLine: s.StartLine, StartCol: s.StartCol,
			EndLine: s.EndLine, EndCol: s.EndCol,
		}
	}
	return out
}

func goSymbolsViaRegex(src []byte) []Symbol {
	text := string(src)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	reMethod := regexp.MustCompile(`^\s*func\s+\([^)]*\)\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reFunc := regexp.MustCompile(`^\s*func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reType := regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

	var out []Symbol
	// at spans the name on 0-based line i from byte offset col, in 1-based positions.
	at := func(name, kind string, i, col int) Symbol {
		return Symbol{
			Name: name, Kind: kind,
			StartLine: i + 1, StartCol: col + 1,
			EndLine: i + 1, EndCol: col + len(name) + 1,
		}
	}

	for i, line := range lines {
		if m := reMethod.FindStringSubmatchIndex(line); m != nil {
			out = append(out, at(line[m[2]:m[3]], "method", i, m[2]))
			continue
		}
		if m := reFunc.FindStringSubmatchIndex(line); m != nil {
			out = append(out, at(line[m[2]:m[3]], "function", i, m[2]))
			continue
		}
		if m := reType.FindStringSubmatchIndex(line); m != nil {
			out = append(out, at(line[m[2]:m[3]], "type", i, m[2]))
		}
	}

	return out
}

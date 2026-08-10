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
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/protocol"
)

type CodeSymbolsRequest struct {
	Path string `json:"path"`
}

type Symbol struct {
	Name  string     `json:"name"`
	Kind  string     `json:"kind"`
	Range *ops.Range `json:"range,omitempty"`
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

func goSymbolsViaRegex(src []byte) []Symbol {
	text := string(src)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	reMethod := regexp.MustCompile(`^\s*func\s+\([^)]*\)\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reFunc := regexp.MustCompile(`^\s*func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reType := regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

	var out []Symbol

	for i, line := range lines {
		if m := reMethod.FindStringSubmatchIndex(line); m != nil {
			name := line[m[2]:m[3]]
			col := m[2]
			out = append(out, Symbol{
				Name: name, Kind: "method",
				Range: &ops.Range{
					Start: ops.Position{Line: i, Col: col},
					End:   ops.Position{Line: i, Col: col + len(name)},
				},
			})
			continue
		}
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

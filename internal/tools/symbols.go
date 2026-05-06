package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

	// Fast path: we only support Go symbols in vNext.
	if !strings.HasSuffix(strings.ToLower(relSlash), ".go") {
		return &CodeSymbolsResponse{Symbols: nil}, nil
	}

	// Hard size limit to avoid memory spikes.
	st, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("path is a directory")
	}
	if st.Size() > 2*1024*1024 {
		// Tier 3: too large -> empty.
		return &CodeSymbolsResponse{Symbols: nil}, nil
	}

	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	// Tier 1: Tree-sitter (if available and parse succeeds).
	if syms, ok := goSymbolsViaTreeSitter(ctx, src); ok {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	// Tier 2: regex heuristics.
	syms := goSymbolsViaRegex(src)
	if len(syms) > 0 {
		return &CodeSymbolsResponse{Symbols: syms}, nil
	}

	// Tier 3: empty.
	_ = filepath.ToSlash // keep filepath imported if build tags change
	return &CodeSymbolsResponse{Symbols: nil}, nil
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
			rng := &ops.Range{
				Start: ops.Position{Line: i, Col: col},
				End:   ops.Position{Line: i, Col: col + len(name)},
			}
			out = append(out, Symbol{Name: name, Kind: "function", Range: rng})
			continue
		}
		if m := reType.FindStringSubmatchIndex(line); m != nil {
			name := line[m[2]:m[3]]
			col := m[2]
			rng := &ops.Range{
				Start: ops.Position{Line: i, Col: col},
				End:   ops.Position{Line: i, Col: col + len(name)},
			}
			out = append(out, Symbol{Name: name, Kind: "type", Range: rng})
			continue
		}
	}

	return out
}

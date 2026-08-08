package tools

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/resolver"
)

func (r *Runner) applyEditSearchReplace(ctx context.Context, relPath string, content []byte, search, replace, targetSymbol string) ([]byte, error) {
	sym := strings.TrimSpace(targetSymbol)
	if sym == "" {
		return resolver.ApplySearchReplace(content, search, replace)
	}
	if start, end, ok := r.symbolLineRange(ctx, relPath, sym); ok {
		return resolver.ApplySearchReplaceWithScope(content, search, replace, start, end)
	}
	return resolver.ApplySearchReplace(content, search, replace)
}

func (r *Runner) symbolLineRange(ctx context.Context, relPath, symbol string) (start, end int, ok bool) {
	if r == nil || r.ckgStore == nil {
		return 0, 0, false
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0, 0, false
	}
	syms, err := r.ckgStore.SymbolsInFile(ctx, relPath)
	if err != nil {
		return 0, 0, false
	}
	want := strings.ToLower(symbol)
	for _, sym := range syms {
		name := strings.ToLower(sym.ShortName)
		if name == want || strings.HasSuffix(name, "."+want) {
			return sym.LineStart, sym.LineEnd, true
		}
	}
	return 0, 0, false
}

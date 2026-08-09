package fs

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/patch/resolver"
)

func (c *Client) applyEditSearchReplace(ctx context.Context, relPath string, content []byte, search, replace, targetSymbol string) ([]byte, error) {
	sym := strings.TrimSpace(targetSymbol)
	if sym == "" {
		return resolver.ApplySearchReplace(content, search, replace)
	}
	if c.Hooks.SymbolLineRange != nil {
		if start, end, ok := c.Hooks.SymbolLineRange(ctx, relPath, sym); ok {
			return resolver.ApplySearchReplaceWithScope(content, search, replace, start, end)
		}
	}
	return resolver.ApplySearchReplace(content, search, replace)
}

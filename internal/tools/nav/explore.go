package nav

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/ckg"
)

type ExploreCodebaseRequest struct {
	SymbolName string `json:"symbol_name"`
	Depth      int    `json:"depth,omitempty"`      // 1..4, default 2
	Direction  string `json:"direction,omitempty"`  // downstream|upstream|both
}

type ExploreCodebaseResponse struct {
	Content string `json:"content"`
}

func (c *Client) ExploreCodebase(ctx context.Context, req ExploreCodebaseRequest) (*ExploreCodebaseResponse, error) {
	var out *ExploreCodebaseResponse
	err := c.withCKG(func(snap CKGAccess) error {
		if snap.Store == nil || snap.Provider == nil {
			return fmt.Errorf("ckg store not initialized")
		}
		orch := ckg.NewOrchestratorWithIgnores(snap.Store, c.Root, c.ExcludeDirs)
		if err := orch.UpdateGraph(ctx); err != nil {
			return fmt.Errorf("update ckg: %w", err)
		}
		content, err := snap.Provider.ExploreSymbol(ctx, req.SymbolName, ckg.ExploreOptions{
			Depth:     req.Depth,
			Direction: req.Direction,
		})
		if err != nil {
			return fmt.Errorf("explore symbol: %w", err)
		}
		out = &ExploreCodebaseResponse{Content: content}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FetchCKGContext returns a <ckg_context> block for step-1 prompt injection:
// ranked FQNs plus a depth-1 neighborhood, capped at ~1500 tokens.
func (c *Client) FetchCKGContext(ctx context.Context, query string) string {
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return ""
	}
	orch := ckg.NewOrchestratorWithIgnores(snap.Store, c.Root, c.ExcludeDirs)
	if err := orch.UpdateGraph(ctx); err != nil {
		return ""
	}
	nodes, err := snap.Store.FindRelevantNodes(ctx, query, 12)
	if err != nil || len(nodes) == 0 {
		return ""
	}
	return snap.Store.FormatPromptContext(ctx, nodes, 1500)
}

// SeedCKGSymbolForTest registers a symbol line range for E2E / eval harnesses.
func (c *Client) SeedCKGSymbolForTest(ctx context.Context, relPath, fileHash, symbol string, lineStart, lineEnd int) error {
	snap, unlock := c.ckgSnap()
	defer unlock()
	if snap.Store == nil {
		return fmt.Errorf("ckg store unavailable")
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return fmt.Errorf("symbol is empty")
	}
	nodes := []ckg.Node{{
		FQN:       "eval." + symbol,
		ShortName: symbol,
		Kind:      "func",
		LineStart: lineStart,
		LineEnd:   lineEnd,
	}}
	snap.Store.LockIndex()
	defer snap.Store.UnlockIndex()
	return snap.Store.SaveFileNodes(ctx, relPath, fileHash, "go", "eval", "eval", nodes, nil)
}

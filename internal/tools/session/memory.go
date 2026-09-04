package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/embed"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/memory"
)

type MemoryWriteRequest struct {
	Content string `json:"content"`
	Scope   string `json:"scope,omitempty"`
}

type MemoryWriteResponse struct {
	Path    string `json:"path"`
	Written int    `json:"written"`
	Scope   string `json:"scope,omitempty"`
}

type MemoryReadRequest struct {
	Layer string `json:"layer,omitempty"`
	Path  string `json:"path,omitempty"`
	MaxKB int    `json:"max_kb,omitempty"`
}

type MemoryReadResponse struct {
	memory.ReadResult
}

type MemorySearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type MemorySearchHit struct {
	Layer   string `json:"layer"`
	Snippet string `json:"snippet"`
}

type MemorySearchResponse struct {
	Hits []MemorySearchHit `json:"hits"`
	// Degraded explains why these are plain substring matches when semantic
	// ranking was configured and then failed. Empty when embeddings are not
	// configured at all (nothing was promised) or when ranking worked.
	Degraded string `json:"degraded,omitempty"`
}

func (c *Client) MemoryWrite(ctx context.Context, req MemoryWriteRequest) (*MemoryWriteResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("session client is nil")
	}
	_ = ctx
	store := c.memoryStore()
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "project"
	}
	rel, n, err := store.Append(scope, req.Content)
	if err != nil {
		return nil, err
	}
	return &MemoryWriteResponse{Path: rel, Written: n, Scope: scope}, nil
}

func (c *Client) MemoryRead(ctx context.Context, req MemoryReadRequest) (*MemoryReadResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("session client is nil")
	}
	_ = ctx
	store := c.memoryStore()
	maxBytes := req.MaxKB * 1024
	if maxBytes <= 0 {
		cfg := c.memCfg()
		maxBytes = cfg.InjectBytes()
	}
	result := store.Read(req.Layer, req.Path, maxBytes)
	return &MemoryReadResponse{ReadResult: result}, nil
}

func (c *Client) MemorySearch(ctx context.Context, req MemorySearchRequest) (*MemorySearchResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("session client is nil")
	}
	_ = ctx
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return nil, fmt.Errorf("query is empty")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 8
	}
	store := c.memoryStore()
	embCfg := c.embedConfig()
	var degraded string
	if strings.TrimSpace(embCfg.Model) != "" {
		hits, err := memory.SemanticSearch(ctx, store, c.Root, q, limit, embed.New(embCfg))
		switch {
		case err != nil:
			// Configuring embed.model promises semantic ranking. Answering with
			// substring matches anyway and saying nothing is how a dead
			// embedding endpoint stays invisible for an entire field run.
			degraded = fmt.Sprintf("semantic ranking unavailable (%v) — results below are substring matches", err)
		case len(hits) > 0:
			out := make([]MemorySearchHit, 0, len(hits))
			for _, h := range hits {
				out = append(out, MemorySearchHit{Layer: h.Layer, Snippet: h.Snippet})
			}
			return &MemorySearchResponse{Hits: out}, nil
		}
	}
	var hits []MemorySearchHit
	add := func(layer, content string) {
		for _, e := range memory.SearchEntries(content, q, limit-len(hits)) {
			snip := e
			if len(snip) > 400 {
				snip = snip[:400] + "…"
			}
			hits = append(hits, MemorySearchHit{Layer: layer, Snippet: snip})
			if len(hits) >= limit {
				return
			}
		}
	}
	if res := store.Read("repo", "", 256*1024); res.Content != "" {
		add("repo", res.Content)
	}
	if res := store.Read("session", "", 128*1024); res.Content != "" && len(hits) < limit {
		add("session", res.Content)
	}
	if res := store.Read("global", "", 64*1024); res.Content != "" && len(hits) < limit {
		add("global", res.Content)
	}
	if res := store.Read("orchestra", "", 64*1024); res.Content != "" && len(hits) < limit {
		add("orchestra", res.Content)
	}
	for _, lh := range lessons.Search(c.Root, q, limit-len(hits)) {
		snip := lh.Snippet
		if lh.Dept != "" {
			snip = "[" + lh.Dept + "] " + snip
		}
		hits = append(hits, MemorySearchHit{Layer: "lessons", Snippet: snip})
		if len(hits) >= limit {
			break
		}
	}
	return &MemorySearchResponse{Hits: hits, Degraded: degraded}, nil
}

func (c *Client) memoryStore() *memory.Store {
	cfg := c.memCfg()
	return memory.NewStore(c.Root, c.sid(), cfg)
}

func (c *Client) AppendSessionMemory(content string) error {
	if c == nil {
		return nil
	}
	_, _, err := c.memoryStore().Append("session", content)
	return err
}

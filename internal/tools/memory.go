package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/memory"
)

// MemoryWriteRequest is the input for the memory_write tool.
type MemoryWriteRequest struct {
	Content string `json:"content"`
	// Scope is "project" (default, .orchestra/memory/agent.md) or "session"
	// (.orchestra/memory/sessions/<session_id>.md).
	Scope string `json:"scope,omitempty"`
}

// MemoryWriteResponse is returned after writing to agent memory.
type MemoryWriteResponse struct {
	Path    string `json:"path"`
	Written int    `json:"written"`
	Scope   string `json:"scope,omitempty"`
}

// MemoryReadRequest is the input for memory_read.
type MemoryReadRequest struct {
	// Layer: orchestra | session | repo | global | all (optional).
	Layer string `json:"layer,omitempty"`
	// Path: e.g. ORCHESTRA.md or .orchestra/memory/agent.md (optional).
	Path string `json:"path,omitempty"`
	// MaxKB caps response size (default from config).
	MaxKB int `json:"max_kb,omitempty"`
}

// MemoryReadResponse is returned by memory_read.
type MemoryReadResponse struct {
	memory.ReadResult
}

// MemoryWrite appends a timestamped entry to project or session memory.
func (r *Runner) MemoryWrite(ctx context.Context, req MemoryWriteRequest) (*MemoryWriteResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	store := r.memoryStore()
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

// MemoryRead lists or reads layered memory on demand (saves context vs eager inject).
func (r *Runner) MemoryRead(ctx context.Context, req MemoryReadRequest) (*MemoryReadResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	_ = ctx
	store := r.memoryStore()
	maxBytes := req.MaxKB * 1024
	if maxBytes <= 0 {
		cfg := r.memoryCfg
		cfg.Normalize()
		maxBytes = cfg.InjectBytes()
	}
	result := store.Read(req.Layer, req.Path, maxBytes)
	return &MemoryReadResponse{ReadResult: result}, nil
}

// MemorySearchRequest is the input for memory_search.
type MemorySearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// MemorySearchHit is one matching memory entry.
type MemorySearchHit struct {
	Layer   string `json:"layer"`
	Snippet string `json:"snippet"`
}

// MemorySearchResponse is returned by memory_search.
type MemorySearchResponse struct {
	Hits []MemorySearchHit `json:"hits"`
}

// MemorySearch scans project/session/global memory for query substrings.
func (r *Runner) MemorySearch(ctx context.Context, req MemorySearchRequest) (*MemorySearchResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
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
	store := r.memoryStore()
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
	return &MemorySearchResponse{Hits: hits}, nil
}

// memoryStore returns the runner's memory store (session-aware).
func (r *Runner) memoryStore() *memory.Store {
	cfg := r.memoryCfg
	cfg.Normalize()
	return memory.NewStore(r.workspaceRoot, r.sessionID, cfg)
}

// AppendSessionMemory writes a one-line note to session-scoped memory (best-effort).
func (r *Runner) AppendSessionMemory(content string) error {
	if r == nil {
		return nil
	}
	_, _, err := r.memoryStore().Append("session", content)
	return err
}

// SetMemoryContext updates session id and config for memory tools/inject.
func (r *Runner) SetMemoryContext(sessionID string, cfg memory.Config) {
	if r == nil {
		return
	}
	cfg.Normalize()
	r.sessionID = strings.TrimSpace(sessionID)
	r.memoryCfg = cfg
}

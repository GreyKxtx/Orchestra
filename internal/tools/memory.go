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

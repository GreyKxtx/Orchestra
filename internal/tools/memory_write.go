package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryWriteRequest is the input for the memory_write tool.
type MemoryWriteRequest struct {
	Content string `json:"content"`
}

// MemoryWriteResponse is returned after writing to agent memory.
type MemoryWriteResponse struct {
	Path    string `json:"path"`
	Written int    `json:"written"`
}

// MemoryWrite appends a timestamped entry to .orchestra/memory/agent.md.
// The file is created if it doesn't exist; the directory is created as needed.
func (r *Runner) MemoryWrite(ctx context.Context, req MemoryWriteRequest) (*MemoryWriteResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("content must not be empty")
	}

	memDir := filepath.Join(r.workspaceRoot, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir .orchestra/memory: %w", err)
	}

	agentFile := filepath.Join(memDir, "agent.md")

	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	entry := fmt.Sprintf("\n---\n*%s*\n\n%s\n", ts, content)

	f, err := os.OpenFile(agentFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open agent.md: %w", err)
	}
	defer f.Close()

	n, err := f.WriteString(entry)
	if err != nil {
		return nil, fmt.Errorf("write agent.md: %w", err)
	}

	relPath := filepath.ToSlash(filepath.Join(".orchestra", "memory", "agent.md"))
	return &MemoryWriteResponse{Path: relPath, Written: n}, nil
}

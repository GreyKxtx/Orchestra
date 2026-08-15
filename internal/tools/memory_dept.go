package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
)

// MemoryWrite routes project/session scopes to agent.md and dept scopes to
// episodic lessons (.orchestra/memory/lessons/<dept>.md).
func (r *Runner) MemoryWrite(ctx context.Context, req MemoryWriteRequest) (*MemoryWriteResponse, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "project"
	}
	if lessons.IsDeptScope(scope) {
		if err := r.consumeDeptLessonWrite(); err != nil {
			return nil, err
		}
		dept := lessons.NormalizeDept(scope)
		if err := lessons.AppendAgentNote(r.workspaceRoot, dept, req.Content); err != nil {
			return nil, err
		}
		rel := filepath.ToSlash(filepath.Join(lessons.RelDir, dept+".md"))
		clipped := lessons.ClipAgentNote(req.Content)
		if clipped == "" {
			return nil, fmt.Errorf("content must not be empty")
		}
		return &MemoryWriteResponse{Path: rel, Written: len(clipped), Scope: dept}, nil
	}
	return r.sessionClient().MemoryWrite(ctx, req)
}

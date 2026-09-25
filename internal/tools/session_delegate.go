package tools

import (
	"context"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools/session"
)

// sessionClientAt is the session tool client for the turn behind ctx: its
// memory notes go to that turn's session.
func (r *Runner) sessionClientAt(ctx context.Context) *session.Client {
	if r == nil {
		return nil
	}
	t := r.TurnAt(ctx)
	return session.NewClient(
		r.workspaceRoot,
		t.SessionID,
		t.MemoryConfig,
		func() config.EmbedConfig { return r.embedCfg },
		func() *ckg.Store {
			r.ckgMu.RLock()
			s := r.ckgStore
			r.ckgMu.RUnlock()
			return s
		},
	)
}

func (r *Runner) MemoryRead(ctx context.Context, req MemoryReadRequest) (*MemoryReadResponse, error) {
	return r.sessionClientAt(ctx).MemoryRead(ctx, req)
}

func (r *Runner) MemorySearch(ctx context.Context, req MemorySearchRequest) (*MemorySearchResponse, error) {
	return r.sessionClientAt(ctx).MemorySearch(ctx, req)
}

func (r *Runner) RuntimeQuery(ctx context.Context, req RuntimeQueryRequest) (*RuntimeQueryResponse, error) {
	return r.sessionClientAt(ctx).RuntimeQuery(ctx, req)
}

// AppendSessionMemory adds a note to the session memory of the turn behind ctx.
func (r *Runner) AppendSessionMemory(ctx context.Context, content string) error {
	return r.sessionClientAt(ctx).AppendSessionMemory(content)
}

// SetMemoryContext names the session and memory configuration of the
// default turn.
func (r *Runner) SetMemoryContext(sessionID string, cfg memory.Config) {
	if r == nil {
		return
	}
	r.turn.SetMemoryContext(sessionID, cfg)
}

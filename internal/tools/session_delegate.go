package tools

import (
	"context"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/tools/session"
)

func (r *Runner) sessionClient() *session.Client {
	if r == nil {
		return nil
	}
	return session.NewClient(
		r.workspaceRoot,
		func() string { return r.sessionID },
		func() memory.Config { return r.memoryCfg },
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
	return r.sessionClient().MemoryRead(ctx, req)
}

func (r *Runner) MemorySearch(ctx context.Context, req MemorySearchRequest) (*MemorySearchResponse, error) {
	return r.sessionClient().MemorySearch(ctx, req)
}

func (r *Runner) RuntimeQuery(ctx context.Context, req RuntimeQueryRequest) (*RuntimeQueryResponse, error) {
	return r.sessionClient().RuntimeQuery(ctx, req)
}

func (r *Runner) AppendSessionMemory(content string) error {
	return r.sessionClient().AppendSessionMemory(content)
}

func (r *Runner) SetMemoryContext(sessionID string, cfg memory.Config) {
	if r == nil {
		return
	}
	cfg.Normalize()
	r.sessionID = sessionID
	r.memoryCfg = cfg
}

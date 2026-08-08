package provision

import "context"

// ProgressEvent reports install progress for status-bar / health polling.
type ProgressEvent struct {
	ID      string // language server id (gopls, …)
	Phase   string // starting | downloading | installing | verifying | done | error
	Percent int    // 0–100; -1 = indeterminate
	Message string
}

// ProgressFunc receives Ensure progress updates (may be called from a worker goroutine).
type ProgressFunc func(ProgressEvent)

type progressCtxKey struct{}

// WithProgress attaches a ProgressFunc to ctx for Ensure / Upgrade.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressCtxKey{}, fn)
}

func progressFrom(ctx context.Context) ProgressFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(progressCtxKey{}).(ProgressFunc)
	return fn
}

func reportProgress(ctx context.Context, ev ProgressEvent) {
	if fn := progressFrom(ctx); fn != nil {
		fn(ev)
	}
}

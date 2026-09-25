package permission

import "context"

type requesterKey struct{}

// WithRequester attributes ctx to the client that answers consent requests
// for what runs under it: a turn's requester rides in the turn's context, so
// a prompt raised by a tool call — an exec.run, a language server to
// install — reaches the client of that turn, not whichever turn last set a
// requester on a shared object. A nil r leaves ctx as it is.
func WithRequester(ctx context.Context, r Requester) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, requesterKey{}, r)
}

// RequesterFrom returns the requester ctx was attributed to, or nil.
func RequesterFrom(ctx context.Context) Requester {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(requesterKey{}).(Requester)
	return r
}

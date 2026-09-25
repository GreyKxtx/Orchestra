package llm

import "context"

// A client is a stack: a provider client under the fallback and router
// decorators. What callers need from it — the request log, the context
// window, the server's limits — is a capability some layer of the stack has,
// not a concrete type (ARCH-8).
//
// Callers used to reach for it with AsOpenAIClient. That made every one of
// these a no-op on an Anthropic client: with provider: anthropic the agent's
// logger was nil on every surface, and llm_log.jsonl stayed empty. These
// helpers walk the stack and ask each layer for the capability.

// maxStackDepth bounds the walk: decorators do not nest deeper than this.
const maxStackDepth = 8

// walkStack calls visit on c and on each layer beneath it until visit
// returns true.
func walkStack(c Client, visit func(Client) bool) {
	for i := 0; i < maxStackDepth && c != nil; i++ {
		if visit(c) {
			return
		}
		u, ok := c.(clientUnwrapper)
		if !ok {
			return
		}
		c = u.Unwrap()
	}
}

// loggerHolder is a client that writes llm_log.jsonl.
type loggerHolder interface {
	SetLogger(*Logger)
	GetLogger() *Logger
}

// LoggerOf returns the request logger of the client stack c, or nil.
func LoggerOf(c Client) *Logger {
	var out *Logger
	walkStack(c, func(l Client) bool {
		if h, ok := l.(loggerHolder); ok && h.GetLogger() != nil {
			out = h.GetLogger()
			return true
		}
		return false
	})
	return out
}

// attachLogger gives logger to the provider client at the bottom of c.
func attachLogger(c Client, logger *Logger) {
	if logger == nil {
		return
	}
	walkStack(c, func(l Client) bool {
		if h, ok := l.(loggerHolder); ok {
			h.SetLogger(logger)
			return true
		}
		return false
	})
}

// ContextTokensOf returns the context window the stack c reports, or 0 when
// no layer knows it. The fallback decorator reports the smaller of its two
// providers' windows, so it is asked before the provider beneath it.
func ContextTokensOf(c Client) int {
	n := 0
	walkStack(c, func(l Client) bool {
		if w, ok := l.(interface{ ContextTokens() int }); ok {
			n = w.ContextTokens()
			return true
		}
		return false
	})
	return n
}

// DiscoverLimits asks the server behind c for the model's limits (context
// window, max output) and applies them, when a layer of the stack can. ok is
// false when none can.
func DiscoverLimits(ctx context.Context, c Client) (limits ModelLimits, ok bool, err error) {
	walkStack(c, func(l Client) bool {
		d, is := l.(interface {
			DiscoverAndApplyLimits(context.Context) (ModelLimits, error)
		})
		if !is {
			return false
		}
		ok = true
		limits, err = d.DiscoverAndApplyLimits(ctx)
		return true
	})
	return limits, ok, err
}

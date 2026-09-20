package web

import (
	"context"
	"encoding/json"
)

// Panel is the browser the person is looking at — the desktop shell's own
// browser view — as opposed to the one `browser.Client` starts for itself.
//
// It is reached through the client that owns it: the core asks, the window
// does it, the answer comes back. That indirection is the point. A browser
// started here is ours and empty; the panel carries the person's session and
// their logins, so it must not be reachable when their window is not there to
// show what is being done with it.
//
// One op vocabulary, shared by every phase of this: `status`, `snapshot`,
// `screenshot` read; `navigate`, `click`, `type`, `fill`, `select`, `close`
// drive; `eval` runs script. What is permitted is decided before the call,
// not here.
type Panel interface {
	// Call performs one op and returns its result object. An op the client
	// cannot do — the panel is closed, the user revoked consent mid-turn —
	// comes back as an error, which is the tool's answer to the model.
	Call(ctx context.Context, op string, params map[string]any) (json.RawMessage, error)
}

// panelKey carries the panel for one run.
//
// The context rather than Config, because a Runner is shared by every client
// of one core while a panel belongs to exactly one of them: a field would be
// a panel from another window, or a race between two turns. A context value
// travels with the turn that is entitled to it and expires with it.
type panelKey struct{}

// WithPanel returns ctx carrying p. The core does this when the turn was asked
// for with the client's own browser and that client offers one.
func WithPanel(ctx context.Context, p Panel) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, panelKey{}, p)
}

// PanelFrom returns the panel this turn may use, or nil. Nil is the ordinary
// case: a CLI run, a headless run, a client with no browser of its own.
func PanelFrom(ctx context.Context) Panel {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(panelKey{}).(Panel)
	return p
}

// panelResult decodes one op's answer into v.
func panelCall(ctx context.Context, p Panel, op string, params map[string]any, v any) error {
	raw, err := p.Call(ctx, op, params)
	if err != nil {
		return err
	}
	if len(raw) == 0 || v == nil {
		return nil
	}
	return json.Unmarshal(raw, v)
}

package web

import (
	"context"
	"encoding/json"

	"github.com/orchestra/orchestra/protocol"
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

// PanelPermits is what this turn may ask the panel to do. Reading is the
// permission to have the panel at all; the other two are asked for separately,
// because they are different acts against the person's own session.
//
// They gate the panel only. A browser the core starts for itself is empty and
// ours, and `--allow-browser` has always meant all ten tools against it;
// narrowing that would be churn without a risk behind it.
type PanelPermits struct {
	// Drive permits navigate, click, type, fill, select, close — acts that
	// change what the person's browser is showing or doing.
	Drive bool
	// Eval permits running the model's own script in the page. Stronger than
	// all the rest together: it is arbitrary code with the person's cookies,
	// so it is asked for on its own and off by default even when driving.
	Eval bool
}

type permitsKey struct{}

// WithPanel returns ctx carrying p. The core does this when the turn was asked
// for with the client's own browser and that client offers one.
func WithPanel(ctx context.Context, p Panel, may PanelPermits) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(context.WithValue(ctx, panelKey{}, p), permitsKey{}, may)
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

// PanelMay reports what this turn may ask of the panel. The zero value —
// read only — is what a turn gets when nothing granted more.
func PanelMay(ctx context.Context) PanelPermits {
	if ctx == nil {
		return PanelPermits{}
	}
	may, _ := ctx.Value(permitsKey{}).(PanelPermits)
	return may
}

// errPanelDenied is the answer to an op this turn was not given. It names the
// switch, because the model cannot grant it and the person can.
func errPanelDenied(op, switchName string) error {
	return protocol.NewError(protocol.ExecDenied,
		"the browser panel refused "+op+": this turn may look at the page but not "+
			switchName+". Ask the person to turn that on in the composer's access menu.",
		map[string]any{"op": op})
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

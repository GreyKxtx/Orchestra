package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/orchestra/orchestra/internal/tools/web"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
	"github.com/orchestra/orchestra/protocol/wire"
)

// Notifier sends server-initiated JSON-RPC notifications to the client.
// *jsonrpc.Server implements this interface.
type Notifier interface {
	Notify(method string, params any) error
}

// RPCHandler adapts Core to the jsonrpc.Handler interface.
type RPCHandler struct {
	core      *Core
	notifier  Notifier // optional; nil = no streaming notifications
	requester func(ctx context.Context, method string, params any, result any) error
	// questionAsker is shared by every source of question/ask — each agent
	// run, each session turn, and MCP elicitation. Its lock only serializes
	// callers holding the same instance, and the collision that matters is
	// between two different sources, so there is exactly one.
	questionAsker *rpcQuestionAsker
	// browserPanel is this connection's own browser — the desktop shell's
	// Browser view. One per connection, because it is that window's.
	browserPanel *rpcBrowserPanel
}

func NewRPCHandler(c *Core) *RPCHandler {
	return &RPCHandler{core: c}
}

// SetNotifier attaches a Notifier so that agent.run can emit streaming events
// as JSON-RPC notifications. Call this after constructing both the Server and handler.
func (h *RPCHandler) SetNotifier(n Notifier) {
	h.notifier = n
}

// SetRequester attaches a request function so that agent.run can issue
// server-initiated requests (e.g. permission/request) to the client. The same
// channel is what MCP servers reach the user through, so it is bound to the
// core's MCP host here — once per connection, matching the servers' lifetime.
func (h *RPCHandler) SetRequester(fn func(ctx context.Context, method string, params any, result any) error) {
	h.requester = fn
	h.questionAsker = &rpcQuestionAsker{requestFn: fn}
	h.browserPanel = &rpcBrowserPanel{requestFn: fn}
	if h.core != nil && h.core.mcpHost != nil {
		h.core.mcpHost.bind(&rpcPermissionRequester{requestFn: fn}, h.questionAsker)
	}
}

// questionAskerForRun is the asker handed to an agent run or a session turn.
// Nil when no client is attached, which disables the question tool — the same
// meaning a nil asker has always had.
//
// Permission requests deliberately keep a per-call requester: the TUI holds a
// real FIFO queue for them (ui/tui/state/permqueue.go), so concurrent consent
// prompts are already handled at the client. Questions have one modal slot and
// no queue, which is why they funnel through a single asker instead.
func (h *RPCHandler) questionAskerForRun() *rpcQuestionAsker {
	if h == nil || h.requester == nil {
		return nil
	}
	return h.questionAsker
}

// browserPanelCtx returns ctx carrying this connection's browser when the
// turn asked for it and there is a client to answer. `browser_panel: true` is
// the client saying it has a browser view open and this turn may use it
// instead of starting one; without it, nothing changes and browser.* reaches
// the Playwright server as before.
func (h *RPCHandler) browserPanelCtx(ctx context.Context, wanted bool, may web.PanelPermits) context.Context {
	if !wanted || h == nil || h.requester == nil || h.browserPanel == nil {
		return ctx
	}
	return web.WithPanel(ctx, h.browserPanel, may)
}

func (h *RPCHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if h == nil || h.core == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	method = strings.TrimSpace(method)
	serve, ok := rpcMethods[method]
	if !ok {
		return nil, jsonrpc.MethodNotFound(method)
	}

	// Handshake requirement: initialize must be called before mutating / tool methods.
	if method != wire.MethodCoreHealth && method != wire.MethodInitialize && !h.core.IsInitialized() {
		return nil, protocol.NewError(protocol.NotInitialized, "initialize required", map[string]any{
			"method": method,
		})
	}

	// Shared-config invariant: if another client (TUI, CLI, manual edit)
	// changed .orchestra.yml since we last read/wrote it, reload before
	// dispatching so read-modify-write persists never clobber external edits.
	if method != wire.MethodInitialize {
		h.core.RefreshConfigIfChanged()
		h.core.applyDiscoveredModelLimits()
	}

	out, err := serve(ctx, h, params)
	var bad *badParams
	if errors.As(err, &bad) {
		return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+bad.Error(), map[string]any{
			"method": method,
		})
	}
	return out, err
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

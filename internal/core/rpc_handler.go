package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/protocol"
)

// Notifier sends server-initiated JSON-RPC notifications to the client.
// *jsonrpc.Server implements this interface.
type Notifier interface {
	Notify(method string, params any) error
}

// RPCHandler adapts Core to the jsonrpc.Handler interface.
type RPCHandler struct {
	core     *Core
	notifier Notifier // optional; nil = no streaming notifications
}

func NewRPCHandler(c *Core) *RPCHandler {
	return &RPCHandler{core: c}
}

// SetNotifier attaches a Notifier so that agent.run can emit streaming events
// as JSON-RPC notifications. Call this after constructing both the Server and handler.
func (h *RPCHandler) SetNotifier(n Notifier) {
	h.notifier = n
}

func (h *RPCHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if h == nil || h.core == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	method = strings.TrimSpace(method)

	// Handshake requirement: initialize must be called before mutating / tool methods.
	if method != "core.health" && method != "initialize" && !h.core.IsInitialized() {
		return nil, protocol.NewError(protocol.NotInitialized, "initialize required", map[string]any{
			"method": method,
		})
	}

	switch method {
	case "core.health":
		return h.core.Health(), nil

	case "initialize":
		var p InitializeParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.Initialize(p)

	case "agent.run":
		var p AgentRunParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		// Wire streaming notifications when a notifier is available.
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		return h.core.AgentRun(ctx, p)

	case "tool.call":
		var p ToolCallParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		out, err := h.core.ToolCall(ctx, p)
		if err != nil {
			return nil, err
		}
		// Return the tool output as a JSON object, not as a JSON-encoded string.
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			return nil, fmt.Errorf("tool output is not valid json: %w", err)
		}
		return v, nil

	case "session.start":
		var p SessionStartParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionStart(p)

	case "session.message":
		var p SessionMessageParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		if h.notifier != nil {
			p.OnEvent = func(method string, params any) {
				_ = h.notifier.Notify(method, params)
			}
		}
		return h.core.SessionMessage(ctx, p)

	case "session.history":
		var p SessionHistoryParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionHistory(p)

	case "session.cancel":
		var p SessionCancelParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return nil, h.core.SessionCancel(p)

	case "session.close":
		var p SessionCloseParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return nil, h.core.SessionClose(p)

	default:
		return nil, jsonrpc.MethodNotFound(method)
	}
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
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

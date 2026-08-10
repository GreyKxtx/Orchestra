package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
)

// Call executes a tool by name with a JSON input object, returning JSON
// output. Wraps callImpl with a deferred recover so a panicking tool body
// (or panicking MCP transport) is converted to an error instead of taking
// down the whole process — agent loops, RPC dispatch, and parallel batch
// workers all reach tools via this entry point. C3 in
// docs/superpowers/plans/2026-05-19-post-audit-refactor.md.
func (r *Runner) Call(ctx context.Context, name string, input json.RawMessage) (out json.RawMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("tool %q panicked: %v\n%s", name, rec, debug.Stack())
		}
	}()
	return r.callImpl(ctx, name, input)
}

// callImpl is the original dispatch body; kept private so all callers go
// through the panic-safe Call wrapper.
func (r *Runner) callImpl(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool name is empty")
	}

	// Route mcp:* calls to the registered MCP manager (use original name).
	if r.mcpCaller != nil && strings.HasPrefix(name, "mcp:") {
		return r.mcpCaller.Call(ctx, name, input)
	}
	return r.callDispatch(ctx, name, input)
}

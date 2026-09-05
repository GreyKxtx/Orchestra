package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/protocol"
)

// Lifecycle hooks let a project act on the turn itself rather than on one tool
// call: refuse a prompt during a release freeze, add the context the model
// cannot know, note in an audit log that a turn finished.

// hooksRunner builds the runner for one session, or nil when hooks are off.
func (c *Core) hooksRunner(sessionID string) *hooks.Runner {
	if c == nil || c.cfg == nil {
		return nil
	}
	return hooks.New(c.cfg.Hooks, c.workspaceRoot).WithSession(sessionID)
}

// applyUserPromptHooks runs user_prompt_submit before the turn starts. A
// denial refuses the turn with the hook's own words; returned context is
// appended to the query, clearly marked as coming from a hook so the model
// does not read it as something the user typed.
func (c *Core) applyUserPromptHooks(ctx context.Context, sessionID, query string) (string, error) {
	r := c.hooksRunner(sessionID)
	if r == nil {
		return query, nil
	}
	payload, _ := json.Marshal(map[string]any{"prompt": query})
	dec := r.RunLifecycle(ctx, hooks.EventUserPromptSubmit, payload)
	if dec.Denied {
		reason := strings.TrimSpace(dec.Reason)
		if reason == "" {
			reason = "a user_prompt_submit hook refused this turn"
		}
		return "", protocol.NewError(protocol.ExecFailed, reason, map[string]any{
			"session_id": sessionID,
			"hook":       hooks.EventUserPromptSubmit,
		})
	}
	if extra := strings.TrimSpace(dec.Context); extra != "" {
		return query + "\n\n<hook_context source=\"user_prompt_submit\">\n" + extra + "\n</hook_context>", nil
	}
	return query, nil
}

// fireLifecycleHook runs a lifecycle event whose outcome cannot change what
// already happened. A denial has nothing left to stop, so it is logged and the
// turn carries on: a failing audit hook must not eat a finished turn's result.
func (c *Core) fireLifecycleHook(ctx context.Context, eventName, sessionID string, payload map[string]any) {
	r := c.hooksRunner(sessionID)
	if r == nil {
		return
	}
	var raw json.RawMessage
	if len(payload) > 0 {
		if b, err := json.Marshal(payload); err == nil {
			raw = b
		}
	}
	if dec := r.RunLifecycle(ctx, eventName, raw); dec.Denied {
		fmt.Fprintf(os.Stderr, "core: %s hook reported: %s\n", eventName, dec.Reason)
	}
}

// turnEndPayload describes how a turn went, including the ones that failed.
func turnEndPayload(res *agent.Result, err error) map[string]any {
	out := map[string]any{"ok": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	if res != nil {
		out["steps"] = res.Steps
		out["applied"] = res.Applied
		out["stop_reason"] = res.StopReason
		out["patches"] = len(res.Patches)
	}
	return out
}

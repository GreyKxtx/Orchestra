package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/llm"
)

// runPreToolHooks runs the pre-tool hook chain with panic protection. A panic
// inside a hook denies the call rather than taking the step down, which is
// what an error from a hook has always done.
func (a *Agent) runPreToolHooks(ctx context.Context, name string, input json.RawMessage) HookDecision {
	var dec HookDecision
	if err := safeRunErr("PreTool hook "+name, func() error {
		dec = a.opts.HooksRunner.RunPreTool(ctx, name, input)
		return nil
	}); err != nil {
		return HookDecision{Denied: true, Reason: err.Error()}
	}
	return dec
}

// firePreCompactHook tells hooks how much history is about to be summarised.
// Fire-and-forget: a hook that fails here must not cost the agent the
// compaction it needs to keep running.
func (a *Agent) firePreCompactHook(ctx context.Context, hist []llm.Message) {
	if a.opts.HooksRunner == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"messages": len(hist),
		"bytes":    historyBytes(hist),
	})
	if err != nil {
		return
	}
	_ = safeRunErr("PreCompact hook", func() error {
		if dec := a.opts.HooksRunner.RunLifecycle(ctx, "pre_compact", payload); dec.Denied {
			a.logf("hook event=pre_compact denied_ignored reason=%s", dec.Reason)
		}
		return nil
	})
}

// hookDenialReason is what the model is told when a hook blocks a call.
//
// "pre-tool hook denied" on its own names the mechanism and not the rule, so
// the model has nothing to correct and calls the same tool again. When the
// hook explains itself, its sentence is the message.
func hookDenialReason(dec HookDecision) string {
	reason := strings.TrimSpace(dec.Reason)
	if reason == "" {
		return "pre-tool hook denied this call"
	}
	return "pre-tool hook denied: " + reason
}

// hookRewriteNote prefixes a tool result whose input a hook replaced. Without
// it the model reads the result as an answer to the arguments it sent, and
// silently learns the wrong thing about the repository.
func hookRewriteNote(dec HookDecision) string {
	note := "[a pre-tool hook rewrote this call's input to " + strings.TrimSpace(string(dec.Input))
	if reason := strings.TrimSpace(dec.Reason); reason != "" {
		note += " — " + reason
	}
	return note + "]\n"
}

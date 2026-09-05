package hooks

import (
	"context"
	"encoding/json"

	"github.com/orchestra/orchestra/internal/config"
)

// Event names. They are the "event" field of the JSON on a hook's stdin and
// the subject a lifecycle hook's matcher is tested against, so one script
// registered for several events can still tell them apart.
const (
	EventPreTool          = "pre_tool"
	EventPostTool         = "post_tool"
	EventSessionStart     = "session_start"
	EventUserPromptSubmit = "user_prompt_submit"
	EventPreCompact       = "pre_compact"
	EventTurnEnd          = "turn_end"
)

// RunLifecycle runs the hooks configured for a lifecycle event.
//
// Tool hooks answer about a tool; these answer about the session. What a
// denial means is the caller's to decide — user_prompt_submit refuses the
// turn, turn_end has nothing left to stop — which is why this returns the
// decision rather than acting on it.
func (r *Runner) RunLifecycle(ctx context.Context, eventName string, payload json.RawMessage) Decision {
	if r == nil {
		return Decision{}
	}
	list := r.listFor(eventName)
	if len(list) == 0 {
		return Decision{}
	}
	return r.runList(ctx, list, eventName, eventName, payload)
}

func (r *Runner) listFor(eventName string) config.HookList {
	switch eventName {
	case EventSessionStart:
		return r.cfg.SessionStart
	case EventUserPromptSubmit:
		return r.cfg.UserPromptSubmit
	case EventPreCompact:
		return r.cfg.PreCompact
	case EventTurnEnd:
		return r.cfg.TurnEnd
	case EventPreTool:
		return r.cfg.PreTool
	case EventPostTool:
		return r.cfg.PostTool
	default:
		return nil
	}
}

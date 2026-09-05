package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestRunLifecycle_RunsTheListForItsOwnEvent(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:      true,
		SessionStart: exitCmd(1),
		TurnEnd:      exitCmd(0),
		TimeoutMS:    5000,
	}, t.TempDir())

	if dec := r.RunLifecycle(context.Background(), EventSessionStart, nil); !dec.Denied {
		t.Fatal("session_start hook did not run")
	}
	if dec := r.RunLifecycle(context.Background(), EventTurnEnd, nil); dec.Denied {
		t.Fatalf("turn_end hook exits 0, got denial: %s", dec.Reason)
	}
}

func TestRunLifecycle_UnconfiguredEventIsANoop(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:      true,
		SessionStart: exitCmd(1),
		TimeoutMS:    5000,
	}, t.TempDir())

	if dec := r.RunLifecycle(context.Background(), EventPreCompact, nil); dec.Denied {
		t.Fatalf("an event with no hooks must not run another event's: %s", dec.Reason)
	}
}

func TestRunLifecycle_NilRunnerIsANoop(t *testing.T) {
	var r *Runner
	if dec := r.RunLifecycle(context.Background(), EventTurnEnd, nil); dec.Denied {
		t.Fatal("nil runner must not deny")
	}
}

// A user_prompt_submit hook exists to add what the model cannot know — the
// deploy freeze, the ticket the branch belongs to. Denying is the lesser half
// of the feature.
func TestRunLifecycle_ContextComesBack(t *testing.T) {
	spec := helperHook(t, "context")
	r := New(config.HooksConfig{
		Enabled:          true,
		UserPromptSubmit: config.HookList{spec},
		TimeoutMS:        20000,
	}, t.TempDir())

	dec := r.RunLifecycle(context.Background(), EventUserPromptSubmit, json.RawMessage(`{"prompt":"fix the build"}`))
	if dec.Denied {
		t.Fatalf("hook allows: %s", dec.Reason)
	}
	if dec.Context != "repo is in a release freeze" {
		t.Fatalf("context = %q", dec.Context)
	}
}

// The matcher of a lifecycle hook is tested against the event name, so one
// hook script can be registered for several events and still opt out.
func TestRunLifecycle_MatcherAppliesToTheEventName(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:      true,
		SessionStart: config.HookList{{Match: "turn_end", Command: exitArgv(1)}},
		TimeoutMS:    5000,
	}, t.TempDir())

	if dec := r.RunLifecycle(context.Background(), EventSessionStart, nil); dec.Denied {
		t.Fatal("matcher naming another event must not run this one")
	}
}

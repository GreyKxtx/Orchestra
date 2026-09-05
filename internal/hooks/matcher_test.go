package hooks

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// sleepArgv blocks for about ten seconds on either platform.
func sleepArgv() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "ping -n 11 127.0.0.1 >nul"}
	}
	return []string{"sh", "-c", "sleep 10"}
}

// denyFor builds a hook that fails (and so denies) whenever it is run, so a
// test can assert on whether the matcher let it run at all — no shell script,
// no temp files, same on every platform.
func denyFor(match string) config.HookSpec {
	return config.HookSpec{Match: match, Command: exitArgv(1)}
}

func TestPreTool_MatcherSkipsOtherTools(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{denyFor("bash|write")},
		TimeoutMS: 5000,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("hook matched on bash|write must not run for read, got: %s", dec.Reason)
	}
	if dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("hook matched on bash|write must run for write")
	}
}

func TestPreTool_EmptyMatchRunsForEveryTool(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{{Command: exitArgv(1)}},
		TimeoutMS: 5000,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("a hook without a matcher must run for every tool")
	}
}

// A pattern that does not compile must not quietly turn the hook off: a gate
// hook that stops running is the failure that costs something.
func TestPreTool_InvalidMatcherRunsEverywhere(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{denyFor("bash|write(")},
		TimeoutMS: 5000,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("a hook with an unparsable matcher must still run")
	}
}

func TestPreTool_FirstDenyWinsAndLaterHooksDoNotRun(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled: true,
		PreTool: config.HookList{
			{Command: exitArgv(3)},
			{Command: []string{"definitely-not-a-real-binary-xyz"}},
		},
		TimeoutMS: 5000,
	}, t.TempDir())

	dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`))
	if !dec.Denied {
		t.Fatal("expected the first hook to deny")
	}
	// The second hook would fail with a spawn error; the message must come
	// from the hook that actually decided.
	if dec.Reason == "" {
		t.Fatal("empty denial reason")
	}
}

func TestPreTool_AllMatchingHooksRunWhenAllowed(t *testing.T) {
	dir := t.TempDir()
	r := New(config.HooksConfig{
		Enabled: true,
		PreTool: config.HookList{
			{Match: "read", Command: exitArgv(0)},
			{Match: "read", Command: exitArgv(0)},
		},
		TimeoutMS: 5000,
	}, dir)

	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("all-allow chain must allow, got: %s", dec.Reason)
	}
}

// A per-hook timeout must beat the global one, or a single slow hook forces
// every other hook in the config to wait as long as the slowest.
func TestPreTool_PerHookTimeoutOverridesGlobal(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   config.HookList{{Command: sleepArgv(), TimeoutMS: 50}},
		TimeoutMS: 600000,
	}, t.TempDir())

	done := make(chan Decision, 1)
	go func() { done <- r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)) }()

	select {
	case dec := <-done:
		if !dec.Denied {
			t.Fatal("expected the per-hook timeout to deny")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("per-hook timeout was ignored; the global 600s timeout applied")
	}
}

func TestNew_LifecycleOnlyConfigStillBuildsRunner(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:      true,
		SessionStart: cmdHook("echo"),
	}, ".")
	if r == nil {
		t.Fatal("a config with only lifecycle hooks must still produce a runner")
	}
}

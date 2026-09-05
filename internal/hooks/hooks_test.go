package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func exitCmd(code int) config.HookList {
	return config.HookList{{Command: exitArgv(code)}}
}

func exitArgv(code int) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", fmt.Sprintf("exit %d", code)}
	}
	return []string{"sh", "-c", fmt.Sprintf("exit %d", code)}
}

func cmdHook(argv ...string) config.HookList {
	return config.HookList{{Command: argv}}
}

func TestNew_DisabledReturnsNil(t *testing.T) {
	r := New(config.HooksConfig{Enabled: false, PreTool: cmdHook("echo")}, ".")
	if r != nil {
		t.Fatal("expected nil when disabled")
	}
}

func TestNew_NoCommandsReturnsNil(t *testing.T) {
	r := New(config.HooksConfig{Enabled: true}, ".")
	if r != nil {
		t.Fatal("expected nil when no commands configured")
	}
}

func TestNew_EnabledWithCommand(t *testing.T) {
	r := New(config.HooksConfig{Enabled: true, PreTool: exitCmd(0)}, ".")
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestRunPreTool_NilRunner(t *testing.T) {
	var r *Runner
	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("nil runner should be no-op, got denial: %s", dec.Reason)
	}
}

func TestRunPreTool_Success(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   exitCmd(0),
		TimeoutMS: 5000,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{"path":"foo.go"}`)); dec.Denied {
		t.Fatalf("expected allow on exit 0, got: %s", dec.Reason)
	}
}

func TestRunPreTool_Failure(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   exitCmd(1),
		TimeoutMS: 5000,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("expected denial on exit 1")
	}
}

func TestRunPostTool_FailureIsNoop(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PostTool:  exitCmd(1),
		TimeoutMS: 5000,
	}, t.TempDir())

	// Should not panic or return error — errors are just logged.
	r.RunPostTool(context.Background(), "write", json.RawMessage(`{}`))
}

func TestRunPreTool_EnvVarsSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env-check script not portable on Windows")
	}

	// Write a script that fails if ORCH_TOOL_NAME is not set.
	dir := t.TempDir()
	script := dir + "/check.sh"
	if err := os.WriteFile(script, []byte(`#!/bin/sh
[ -n "$ORCH_TOOL_NAME" ] || exit 1
[ -n "$ORCH_WORKSPACE_ROOT" ] || exit 1
exit 0
`), 0755); err != nil {
		t.Fatal(err)
	}

	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   cmdHook(script),
		TimeoutMS: 5000,
	}, dir)

	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("env vars not set: %s", dec.Reason)
	}
}

func TestRunPreTool_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep not portable on Windows")
	}
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   cmdHook("sh", "-c", "sleep 10"),
		TimeoutMS: 50,
	}, t.TempDir())

	if dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("expected timeout to deny")
	}
}

func TestBuildEnv(t *testing.T) {
	env := buildEnv("read", json.RawMessage(`{"path":"x"}`), "/workspace", "sess-1", "pre_tool")
	want := map[string]bool{
		"ORCH_TOOL_NAME=read":            true,
		`ORCH_TOOL_INPUT={"path":"x"}`:   true,
		"ORCH_WORKSPACE_ROOT=/workspace": true,
		"ORCH_SESSION_ID=sess-1":         true,
		"ORCH_HOOK_EVENT=pre_tool":       true,
	}
	for _, e := range env {
		delete(want, e)
	}
	if len(want) > 0 {
		t.Fatalf("missing env vars: %v", want)
	}
}

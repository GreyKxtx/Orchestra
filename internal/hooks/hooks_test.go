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

func exitCmd(code int) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", fmt.Sprintf("exit %d", code)}
	}
	return []string{"sh", "-c", fmt.Sprintf("exit %d", code)}
}

func TestNew_DisabledReturnsNil(t *testing.T) {
	r := New(config.HooksConfig{Enabled: false, PreTool: []string{"echo"}}, ".")
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
	err := r.RunPreTool(context.Background(), "fs.read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("nil runner should be no-op, got: %v", err)
	}
}

func TestRunPreTool_Success(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   exitCmd(0),
		TimeoutMS: 5000,
	}, t.TempDir())

	err := r.RunPreTool(context.Background(), "fs.write", json.RawMessage(`{"path":"foo.go"}`))
	if err != nil {
		t.Fatalf("expected nil error on exit 0, got: %v", err)
	}
}

func TestRunPreTool_Failure(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   exitCmd(1),
		TimeoutMS: 5000,
	}, t.TempDir())

	err := r.RunPreTool(context.Background(), "fs.write", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error on exit 1")
	}
}

func TestRunPostTool_FailureIsNoop(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PostTool:  exitCmd(1),
		TimeoutMS: 5000,
	}, t.TempDir())

	// Should not panic or return error — errors are just logged.
	r.RunPostTool(context.Background(), "fs.write", json.RawMessage(`{}`))
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
		PreTool:   []string{script},
		TimeoutMS: 5000,
	}, dir)

	err := r.RunPreTool(context.Background(), "fs.read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("env vars not set: %v", err)
	}
}

func TestRunPreTool_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep not portable on Windows")
	}
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   []string{"sh", "-c", "sleep 10"},
		TimeoutMS: 50,
	}, t.TempDir())

	err := r.RunPreTool(context.Background(), "fs.write", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestBuildEnv(t *testing.T) {
	env := buildEnv("fs.read", json.RawMessage(`{"path":"x"}`), "/workspace")
	want := map[string]bool{
		"ORCH_TOOL_NAME=fs.read":          true,
		`ORCH_TOOL_INPUT={"path":"x"}`:    true,
		"ORCH_WORKSPACE_ROOT=/workspace":  true,
	}
	for _, e := range env {
		delete(want, e)
	}
	if len(want) > 0 {
		t.Fatalf("missing env vars: %v", want)
	}
}

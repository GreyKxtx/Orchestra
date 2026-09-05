package hooks

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// TestHookHelperProcess is not a test. It is the subprocess the protocol tests
// spawn as a hook, so the fixtures are Go instead of shell scripts and run the
// same on Windows and Unix.
func TestHookHelperProcess(t *testing.T) {
	mode := os.Getenv("ORCH_HOOK_HELPER_MODE")
	if mode == "" {
		t.Skip("helper process; only runs when spawned as a hook")
	}
	stdin, _ := io.ReadAll(os.Stdin)
	switch mode {
	case "capture-stdin":
		_ = os.WriteFile(os.Getenv("ORCH_HOOK_HELPER_OUT"), stdin, 0o644)
	case "deny":
		os.Stdout.WriteString(`{"decision":"deny","reason":"no writes on Friday"}`)
	case "allow":
		os.Stdout.WriteString(`{"decision":"allow"}`)
	case "modify":
		os.Stdout.WriteString(`{"decision":"modify","input":{"path":"safe.txt"},"reason":"redirected"}`)
	case "modify-garbage":
		os.Stdout.WriteString(`{"decision":"modify","input":"not-an-object"}`)
	case "noise":
		os.Stdout.WriteString("hook ran fine\nnothing to report\n")
	case "context":
		os.Stdout.WriteString(`{"decision":"allow","context":"repo is in a release freeze"}`)
	}
	os.Exit(0)
}

// helperHook builds a hook spec that re-invokes this test binary as the hook
// subprocess in the given mode.
func helperHook(t *testing.T, mode string) config.HookSpec {
	t.Helper()
	t.Setenv("ORCH_HOOK_HELPER_MODE", mode)
	return config.HookSpec{Command: []string{os.Args[0], "-test.run=TestHookHelperProcess"}}
}

func helperRunner(t *testing.T, mode string) *Runner {
	t.Helper()
	spec := helperHook(t, mode)
	return New(config.HooksConfig{Enabled: true, PreTool: config.HookList{spec}, TimeoutMS: 20000}, t.TempDir())
}

// The hook has to be told what it is deciding about. Env vars carry the tool
// name and input already; stdin carries the whole event so a hook can be
// written in any language without unpacking three variables.
func TestPreTool_StdinCarriesTheEvent(t *testing.T) {
	out := t.TempDir() + "/payload.json"
	t.Setenv("ORCH_HOOK_HELPER_OUT", out)
	r := helperRunner(t, "capture-stdin")
	r = r.WithSession("sess-42")

	dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{"path":"a.txt"}`))
	if dec.Denied {
		t.Fatalf("helper allows, got denial: %s", dec.Reason)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("hook received no stdin: %v", err)
	}
	var ev struct {
		Event         string          `json:"event"`
		Tool          string          `json:"tool"`
		Input         json.RawMessage `json:"input"`
		SessionID     string          `json:"session_id"`
		WorkspaceRoot string          `json:"workspace_root"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("stdin is not JSON: %v (%s)", err, raw)
	}
	if ev.Event != "pre_tool" || ev.Tool != "write" {
		t.Fatalf("event = %q tool = %q", ev.Event, ev.Tool)
	}
	if !strings.Contains(string(ev.Input), `"path":"a.txt"`) {
		t.Fatalf("input = %s", ev.Input)
	}
	if ev.SessionID != "sess-42" {
		t.Fatalf("session_id = %q", ev.SessionID)
	}
	if ev.WorkspaceRoot == "" {
		t.Fatal("workspace_root missing")
	}
}

func TestPreTool_JSONDenyCarriesReasonToTheModel(t *testing.T) {
	r := helperRunner(t, "deny")
	dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{}`))
	if !dec.Denied {
		t.Fatal("decision:deny on stdout must deny the call")
	}
	if !strings.Contains(dec.Reason, "no writes on Friday") {
		t.Fatalf("the hook's own reason must reach the model, got %q", dec.Reason)
	}
}

func TestPreTool_JSONAllowRuns(t *testing.T) {
	r := helperRunner(t, "allow")
	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("decision:allow must allow, got %q", dec.Reason)
	}
}

// A hook that just logs its progress must not be read as a decision. Every
// hook written before the protocol existed prints something.
func TestPreTool_NonJSONOutputIsNotADecision(t *testing.T) {
	r := helperRunner(t, "noise")
	if dec := r.RunPreTool(context.Background(), "read", json.RawMessage(`{}`)); dec.Denied {
		t.Fatalf("chatty hook must not deny, got %q", dec.Reason)
	}
}

func TestPreTool_ModifyReplacesTheToolInput(t *testing.T) {
	r := helperRunner(t, "modify")
	dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{"path":"secret.txt"}`))
	if dec.Denied {
		t.Fatalf("modify must not deny: %s", dec.Reason)
	}
	if !strings.Contains(string(dec.Input), `"path":"safe.txt"`) {
		t.Fatalf("input not replaced: %s", dec.Input)
	}
}

// Anything but a JSON object would fail tool-schema validation later, with an
// error that points at the model instead of at the hook that caused it.
func TestPreTool_ModifyWithNonObjectInputIsIgnored(t *testing.T) {
	r := helperRunner(t, "modify-garbage")
	dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{"path":"a.txt"}`))
	if dec.Denied {
		t.Fatalf("garbage rewrite must not deny: %s", dec.Reason)
	}
	if len(dec.Input) != 0 {
		t.Fatalf("non-object rewrite must be ignored, got %s", dec.Input)
	}
}

// Exit codes stay the contract they were: this is what every hook written
// before the JSON protocol relies on.
func TestPreTool_NonZeroExitStillDenies(t *testing.T) {
	r := New(config.HooksConfig{
		Enabled:   true,
		PreTool:   exitCmd(1),
		TimeoutMS: 5000,
	}, t.TempDir())
	if dec := r.RunPreTool(context.Background(), "write", json.RawMessage(`{}`)); !dec.Denied {
		t.Fatal("non-zero exit must still deny")
	}
}

// Hooks used to run with a three-variable environment, so a script that
// called git or node found neither on PATH.
func TestHookEnv_InheritsTheParentEnvironment(t *testing.T) {
	t.Setenv("ORCH_HOOK_ENV_PROBE", "present")
	env := buildEnv("read", json.RawMessage(`{}`), "/workspace", "sess-1", "pre_tool")
	var sawProbe, sawTool bool
	for _, e := range env {
		if e == "ORCH_HOOK_ENV_PROBE=present" {
			sawProbe = true
		}
		if e == "ORCH_TOOL_NAME=read" {
			sawTool = true
		}
	}
	if !sawProbe {
		t.Fatal("hook environment must inherit the parent's, or PATH is empty in the hook")
	}
	if !sawTool {
		t.Fatal("ORCH_TOOL_NAME missing")
	}
}

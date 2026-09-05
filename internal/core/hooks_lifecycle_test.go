package core

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

// TestCoreHookHelperProcess is not a test: it is the subprocess the lifecycle
// tests register as a hook, so the fixtures are Go instead of shell and behave
// the same on Windows and Unix.
func TestCoreHookHelperProcess(t *testing.T) {
	mode := os.Getenv("ORCH_CORE_HOOK_MODE")
	if mode == "" {
		t.Skip("helper process; only runs when spawned as a hook")
	}
	stdin, _ := io.ReadAll(os.Stdin)
	switch mode {
	case "capture":
		_ = os.WriteFile(os.Getenv("ORCH_CORE_HOOK_OUT"), stdin, 0o644)
	case "context":
		os.Stdout.WriteString(`{"decision":"allow","context":"the repo is in a release freeze"}`)
	case "deny":
		os.Stdout.WriteString(`{"decision":"deny","reason":"this branch is read-only until the release lands"}`)
	}
	os.Exit(0)
}

func coreWithHook(t *testing.T, mode string, set func(*config.HooksConfig, config.HookList)) *Core {
	t.Helper()
	t.Setenv("ORCH_CORE_HOOK_MODE", mode)
	list := config.HookList{{Command: []string{os.Args[0], "-test.run=TestCoreHookHelperProcess"}}}
	hooksCfg := config.HooksConfig{Enabled: true, TimeoutMS: 20000}
	set(&hooksCfg, list)
	return &Core{
		workspaceRoot: t.TempDir(),
		cfg:           &config.ProjectConfig{Hooks: hooksCfg},
	}
}

// A user_prompt_submit hook earns its place by adding what the model cannot
// know — a freeze, a ticket, an on-call rotation. If that text does not reach
// the turn, the hook is only a slower way to say no.
func TestUserPromptHooks_ContextReachesTheTurn(t *testing.T) {
	c := coreWithHook(t, "context", func(h *config.HooksConfig, l config.HookList) { h.UserPromptSubmit = l })

	query, err := c.applyUserPromptHooks(context.Background(), "sess-1", "fix the build")
	if err != nil {
		t.Fatalf("hook allows: %v", err)
	}
	if !strings.Contains(query, "fix the build") {
		t.Fatalf("the user's own words must survive: %q", query)
	}
	if !strings.Contains(query, "the repo is in a release freeze") {
		t.Fatalf("hook context never reached the turn: %q", query)
	}
}

func TestUserPromptHooks_DenialStopsTheTurnWithTheReason(t *testing.T) {
	c := coreWithHook(t, "deny", func(h *config.HooksConfig, l config.HookList) { h.UserPromptSubmit = l })

	_, err := c.applyUserPromptHooks(context.Background(), "sess-1", "ship it")
	if err == nil {
		t.Fatal("a denied prompt must not start a turn")
	}
	if !strings.Contains(err.Error(), "read-only until the release lands") {
		t.Fatalf("the hook's reason must reach the user: %v", err)
	}
}

func TestUserPromptHooks_NoHooksLeavesTheQueryAlone(t *testing.T) {
	c := &Core{workspaceRoot: t.TempDir(), cfg: &config.ProjectConfig{}}
	query, err := c.applyUserPromptHooks(context.Background(), "sess-1", "fix the build")
	if err != nil {
		t.Fatal(err)
	}
	if query != "fix the build" {
		t.Fatalf("query = %q", query)
	}
}

func TestSessionStartHook_FiresWithTheSessionID(t *testing.T) {
	out := filepath.Join(t.TempDir(), "event.json")
	t.Setenv("ORCH_CORE_HOOK_OUT", out)
	c := coreWithHook(t, "capture", func(h *config.HooksConfig, l config.HookList) { h.SessionStart = l })

	c.fireLifecycleHook(context.Background(), "session_start", "sess-7", map[string]any{})

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("session_start hook never ran: %v", err)
	}
	var ev struct {
		Event     string `json:"event"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("hook stdin is not JSON: %v (%s)", err, raw)
	}
	if ev.Event != "session_start" || ev.SessionID != "sess-7" {
		t.Fatalf("event = %q session = %q", ev.Event, ev.SessionID)
	}
}

// turn_end has nothing left to stop, so a denial is only worth a log line —
// but the payload has to say how the turn went, or the hook cannot report it.
func TestTurnEndHook_PayloadCarriesTheOutcome(t *testing.T) {
	out := filepath.Join(t.TempDir(), "event.json")
	t.Setenv("ORCH_CORE_HOOK_OUT", out)
	c := coreWithHook(t, "capture", func(h *config.HooksConfig, l config.HookList) { h.TurnEnd = l })

	c.fireLifecycleHook(context.Background(), "turn_end", "sess-7", map[string]any{
		"steps":       3,
		"stop_reason": "completed",
		"applied":     true,
	})

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("turn_end hook never ran: %v", err)
	}
	var ev struct {
		Event string `json:"event"`
		Input struct {
			Steps      int    `json:"steps"`
			StopReason string `json:"stop_reason"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("hook stdin is not JSON: %v (%s)", err, raw)
	}
	if ev.Event != "turn_end" || ev.Input.Steps != 3 || ev.Input.StopReason != "completed" {
		t.Fatalf("payload = %s", raw)
	}
}

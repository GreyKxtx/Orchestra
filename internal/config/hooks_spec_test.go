package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseHooks(t *testing.T, body string) HooksConfig {
	t.Helper()
	var wrapper struct {
		Hooks HooksConfig `yaml:"hooks"`
	}
	if err := yaml.Unmarshal([]byte(body), &wrapper); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return wrapper.Hooks
}

// The single-command form is what every existing config on disk uses. It must
// keep parsing byte-for-byte the same, or upgrading Orchestra silently turns
// off a user's gate hook.
func TestHookList_LegacyCommandFormParses(t *testing.T) {
	h := parseHooks(t, `
hooks:
  enabled: true
  pre_tool: ["sh", "-c", "exit 0"]
`)
	if len(h.PreTool) != 1 {
		t.Fatalf("want 1 hook, got %d (%#v)", len(h.PreTool), h.PreTool)
	}
	if got := strings.Join(h.PreTool[0].Command, " "); got != "sh -c exit 0" {
		t.Fatalf("command = %q", got)
	}
	if h.PreTool[0].Match != "" {
		t.Fatalf("legacy hook must match every tool, got match=%q", h.PreTool[0].Match)
	}
}

func TestHookList_MatcherFormParses(t *testing.T) {
	h := parseHooks(t, `
hooks:
  enabled: true
  pre_tool:
    - match: "bash|write"
      command: ["./gate.sh"]
      timeout_ms: 2000
    - command: ["./audit.sh"]
`)
	if len(h.PreTool) != 2 {
		t.Fatalf("want 2 hooks, got %d (%#v)", len(h.PreTool), h.PreTool)
	}
	if h.PreTool[0].Match != "bash|write" {
		t.Fatalf("match = %q", h.PreTool[0].Match)
	}
	if h.PreTool[0].TimeoutMS != 2000 {
		t.Fatalf("timeout_ms = %d", h.PreTool[0].TimeoutMS)
	}
	if len(h.PreTool[1].Command) != 1 || h.PreTool[1].Command[0] != "./audit.sh" {
		t.Fatalf("second hook command = %#v", h.PreTool[1].Command)
	}
}

func TestHookList_LifecycleEventsParse(t *testing.T) {
	h := parseHooks(t, `
hooks:
  enabled: true
  session_start: ["./start.sh"]
  user_prompt_submit:
    - command: ["./context.sh"]
  pre_compact: ["./before-compact.sh"]
  turn_end: ["./after-turn.sh"]
`)
	for name, list := range map[string]HookList{
		"session_start":      h.SessionStart,
		"user_prompt_submit": h.UserPromptSubmit,
		"pre_compact":        h.PreCompact,
		"turn_end":           h.TurnEnd,
	} {
		if len(list) != 1 {
			t.Fatalf("%s: want 1 hook, got %d", name, len(list))
		}
	}
}

// A settings round-trip through the TUI re-marshals the whole config. A user
// who wrote the legacy one-command form should not find their file rewritten
// into a shape they did not choose.
func TestHookList_MarshalKeepsLegacyShape(t *testing.T) {
	h := parseHooks(t, `
hooks:
  enabled: true
  pre_tool: ["sh", "-c", "exit 0"]
`)
	out, err := yaml.Marshal(map[string]any{"hooks": h})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	round := parseHooks(t, string(out))
	if len(round.PreTool) != 1 || strings.Join(round.PreTool[0].Command, " ") != "sh -c exit 0" {
		t.Fatalf("round trip lost the command: %s", out)
	}
	if strings.Contains(string(out), "command:") {
		t.Fatalf("legacy form should stay a plain string list, got:\n%s", out)
	}
}

// An empty or malformed entry must not become a hook that spawns an empty
// command on every tool call.
func TestHookList_EmptyEntriesAreDropped(t *testing.T) {
	h := parseHooks(t, `
hooks:
  enabled: true
  pre_tool:
    - match: "bash"
      command: []
    - command: ["./real.sh"]
`)
	if len(h.PreTool) != 1 || h.PreTool[0].Command[0] != "./real.sh" {
		t.Fatalf("want only the real hook, got %#v", h.PreTool)
	}
}

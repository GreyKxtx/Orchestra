package prompt

import (
	"strings"
	"testing"
)

// TestMemoryGuidanceReachesModesThatHaveTheTool is the direct fix for the
// field run's headline finding: memory_write was called 0 times across 91
// turns, because no build/general prompt ever told the model when to use
// it — the tool existed, the trigger did not.
//
// Only modes whose tool list actually carries memory_write are covered
// (internal/tools/registry.go): plan mode does not get memory_write at all,
// so guidance there would point at a tool the model cannot call.
func TestMemoryGuidanceReachesModesThatHaveTheTool(t *testing.T) {
	cases := []struct{ mode, family string }{
		{"build", "default"},
		{"build", "anthropic"},
		{"build", "gpt"},
		{"build", "gemini"},
		{"build", "kimi"},
		{"build", "local"},
		{"general", "default"},
		{"general", "local"},
	}
	for _, c := range cases {
		got := BuildSystemPromptForMode(c.mode, c.family)
		if !strings.Contains(got, "memory_write") {
			t.Errorf("mode=%s family=%s: no memory_write guidance:\n%s", c.mode, c.family, got)
		}
		if !strings.Contains(got, "scope=project") || !strings.Contains(got, "scope=session") {
			t.Errorf("mode=%s family=%s: guidance must cover both project and session scope", c.mode, c.family)
		}
	}
}

// TestMemoryGuidanceAbsentWherePlanHasNoTheTool guards the negative case:
// plan mode's tool list (registry.go listToolsPlan) has no memory_write, so
// the prompt must not tell the model to call it.
func TestMemoryGuidanceAbsentWherePlanHasNoTheTool(t *testing.T) {
	for _, family := range []string{"default", "local"} {
		got := BuildSystemPromptForMode("plan", family)
		if strings.Contains(got, "memory_write") {
			t.Errorf("plan/%s: prompt mentions memory_write, but plan mode has no such tool", family)
		}
	}
}

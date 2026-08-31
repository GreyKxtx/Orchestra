package prompt

import (
	"strings"
	"testing"
)

// Primary agent modes must resolve to a non-empty embedded system prompt.
func TestAllPrimaryModesHaveSystemPrompt(t *testing.T) {
	modes := []string{
		"build", "plan", "explore", "ask", "debug", "architecture",
		"general", "orchestra", "worker", "verifier",
		"compaction", "title", "summary",
	}
	for _, mode := range modes {
		got := BuildSystemPromptForMode(mode, "default")
		if strings.TrimSpace(got) == "" {
			t.Fatalf("mode %q: empty system prompt", mode)
		}
	}
}

func TestOrchestraPrompt_MentionsDelegation(t *testing.T) {
	got := BuildSystemPromptForMode("orchestra", "default")
	for _, needle := range []string{"worker", "WorkOrder", "task", "Do NOT edit", "direction='upstream'", "lesson_promote", "playbook_promote"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("orchestra prompt missing %q:\n%s", needle, got)
		}
	}
}

func TestWorkerPrompt_MentionsTaskResult(t *testing.T) {
	got := BuildSystemPromptForMode("worker", "default")
	if !strings.Contains(got, "task_result") {
		t.Fatalf("worker prompt must mention task_result:\n%s", got)
	}
}

// TestPromptFilesPlanPathPlaceholder documents which prompts carry the
// {{PLAN_PATH}} placeholder — every one of them must be run through
// agent.substitutePlanPath before it reaches a model.
func TestPromptFilesPlanPathPlaceholder(t *testing.T) {
	withPlaceholder := []string{"plan", "plan-local", "architecture"}
	for _, mode := range withPlaceholder {
		if s := LoadEmbedded(mode + ".txt"); !strings.Contains(s, "{{PLAN_PATH}}") {
			t.Errorf("%s.txt no longer uses {{PLAN_PATH}} — update the substitution site in agent.buildSystemPrompt", mode)
		}
	}
}

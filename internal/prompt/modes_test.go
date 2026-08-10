package prompt

import (
	"strings"
	"testing"
)

// Primary agent modes must resolve to a non-empty embedded system prompt.
func TestAllPrimaryModesHaveSystemPrompt(t *testing.T) {
	modes := []string{
		"build", "plan", "explore", "ask", "debug", "architecture",
		"general", "orchestra", "worker",
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
	for _, needle := range []string{"worker", "WorkOrder", "task", "Do NOT edit"} {
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

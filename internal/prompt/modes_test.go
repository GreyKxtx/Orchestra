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

// TestPromptFilesAreEnglish keeps the prompt set in one language.
//
// The set used to be split: build*/plan/explore/ask/debug/general/architecture/
// verifier/compaction/summary/title were Russian while orchestra/worker/product/
// documentation/task/todowrite/auto-router were English, so a single run mixed
// languages (Lead → worker → verifier). A prompt's language also leaks into the
// model's output — Russian commit messages and comments in an English codebase —
// and Cyrillic costs more tokens on the local tokenizers these prompts target.
func TestPromptFilesAreEnglish(t *testing.T) {
	entries, err := promptFiles.ReadDir("files")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body := loadPromptFile(e.Name())
		if body == "" {
			t.Errorf("%s: empty prompt file", e.Name())
			continue
		}
		for _, r := range body {
			if (r >= 0x0400 && r <= 0x04FF) || (r >= 0x0500 && r <= 0x052F) {
				t.Errorf("%s: contains Cyrillic (%q) — prompts are English", e.Name(), string(r))
				break
			}
		}
	}
}

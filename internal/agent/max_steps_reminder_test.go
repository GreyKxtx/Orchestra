package agent

import (
	"strings"
	"testing"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
)

// LLM-16: the step-limit reminder asked every run for "the final PatchSet
// JSON". A mode that writes nothing ends with its answer, not a PatchSet.
func TestMaxStepsReminder_FitsTheRun(t *testing.T) {
	for mode, want := range map[Mode]string{
		ModeBuild:   "PatchSet",
		ModeAsk:     "answer with what you have found",
		ModeExplore: "answer with what you have found",
	} {
		ag := newTimeoutAgent(t, hangLLM{}, Options{Mode: mode})
		got := ag.maxStepsReminder()
		if !strings.Contains(got, want) {
			t.Errorf("%s: reminder %q, want %q", mode, got, want)
		}
		if mode != ModeBuild && strings.Contains(got, "PatchSet") {
			t.Errorf("%s asks for a PatchSet: %q", mode, got)
		}
	}
}

// The general and explore prompts told a main agent to end with task_result,
// a tool it is not offered there: the model did as told, was refused, and had
// no way left to end the turn.
func TestGeneralAndExplorePrompts_EndAMainRunWithoutTaskResult(t *testing.T) {
	for _, mode := range []string{"general", "explore"} {
		p := promptpkg.LoadEmbedded(mode + ".txt")
		if !strings.Contains(p, "if it is in tools[]") && !strings.Contains(p, "if task_result is in tools[]") {
			t.Errorf("%s.txt makes task_result unconditional:\n%s", mode, p)
		}
		if !strings.Contains(p, "main agent") {
			t.Errorf("%s.txt does not say how a main agent ends the turn", mode)
		}
	}
}

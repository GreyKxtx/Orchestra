package prompt

import (
	"strings"
	"testing"
)

// Every build turn is offered todowrite and todoread — the tool list does not
// vary by model family. The prompts did: only build-anthropic and build-local
// ever mentioned the checklist, so a model on the gemini, gpt, kimi or default
// prompt was handed a tool nothing told it to use, and duly never used it.
//
// Two eval runs showed the shape of it: asked in so many words to "track the
// three as a todo list with todowrite as you go", both a Gemini model (gemini
// family) and one that matches no family at all (default, build.txt) made all
// three edits correctly and called todowrite zero times.
func TestBuildPrompts_EveryFamilyIsToldAboutTheTodoList(t *testing.T) {
	entries, err := promptFiles.ReadDir("files")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "build") || !strings.HasSuffix(name, ".txt") {
			continue
		}
		body, err := promptFiles.ReadFile("files/" + name)
		if err != nil {
			t.Fatal(err)
		}
		seen++
		if !strings.Contains(string(body), "todowrite") {
			t.Errorf("%s never mentions todowrite, but every build turn is offered it", name)
		}
	}
	if seen < 5 {
		t.Errorf("only %d build prompts found; the walk is not covering the set", seen)
	}
}

// build-local told the model to finish one todo and end the turn — "the next
// todo belongs to a NEW user turn" — while the final guard refuses a final with
// an open todo. A model that obeyed the prompt was refused; one that obeyed
// the guard ignored the prompt. Live runs on a local 27B did the whole list in
// one turn either way, so the prompts say that now.
func TestBuildPrompts_DoTheWholeChecklistInOneTurn(t *testing.T) {
	for _, name := range []string{"build-local.txt", "build.txt"} {
		body, err := promptFiles.ReadFile("files/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		if strings.Contains(s, "NEW user turn") || strings.Contains(s, "One user turn = one todo") {
			t.Errorf("%s still ends a turn on an open checklist", name)
		}
		if !strings.Contains(s, "report in the user's language") {
			t.Errorf("%s does not ask for a report after the edits", name)
		}
	}
}

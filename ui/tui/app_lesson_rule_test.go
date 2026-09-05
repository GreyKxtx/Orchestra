package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestShowRuleSuggestion_OpensModalWithLessonRuleKind(t *testing.T) {
	a := testChromeApp(t)
	sug := &rpcclient.RuleSuggestion{Dept: "engineering", File: "src/App.jsx", Count: 3, Text: "3x StaleContent on src/App.jsx"}

	a.showRuleSuggestion(sug)

	if a.permModal == nil || a.permModal.Kind != "lesson_rule" {
		t.Fatalf("permModal = %+v, want a lesson_rule modal", a.permModal)
	}
	if a.ruleSuggestion != sug {
		t.Error("ruleSuggestion must be recorded for the response to answer with")
	}
}

// A rule suggestion is low-urgency: if an exec/lsp permission is already
// pending, it must not steal the modal slot — it fires again next time.
func TestShowRuleSuggestion_SkipsWhenAnotherModalIsShowing(t *testing.T) {
	a := testChromeApp(t)
	a.permModal = view.NewPermissionModal("bash", "go test", "")

	a.showRuleSuggestion(&rpcclient.RuleSuggestion{File: "src/App.jsx"})

	if a.ruleSuggestion != nil {
		t.Error("must not record a suggestion it isn't going to show")
	}
	if a.permModal.Kind == "lesson_rule" {
		t.Fatal("must not overwrite an already-showing permission modal")
	}
}

func TestRespondRuleSuggestion_AcceptSendsRPCAndClearsModal(t *testing.T) {
	a, f := testCoreApp(t)
	sug := &rpcclient.RuleSuggestion{Dept: "engineering", File: "src/App.jsx", Verify: "StaleContent", RuleLine: "read first", Text: "3x on src/App.jsx"}
	a.showRuleSuggestion(sug)

	cmd := a.respondRuleSuggestion(true)
	if a.permModal != nil || a.ruleSuggestion != nil {
		t.Fatal("modal and pending suggestion must be cleared immediately")
	}
	execCmdTree(cmd)

	answers := f.ruleSuggestionAnswers
	if len(answers) != 1 || !answers[0].Accept {
		t.Fatalf("answers = %+v, want one Accept=true", answers)
	}
	if answers[0].Sug.File != "src/App.jsx" || answers[0].Sug.RuleLine != "read first" {
		t.Errorf("answer did not carry the suggestion through: %+v", answers[0].Sug)
	}
}

func TestRespondRuleSuggestion_DeclineSendsRPCAndClearsModal(t *testing.T) {
	a, f := testCoreApp(t)
	a.showRuleSuggestion(&rpcclient.RuleSuggestion{Dept: "engineering", File: "src/App.jsx"})

	execCmdTree(a.respondRuleSuggestion(false))

	answers := f.ruleSuggestionAnswers
	if len(answers) != 1 || answers[0].Accept {
		t.Fatalf("answers = %+v, want one Accept=false", answers)
	}
}

func TestRespondRuleSuggestion_NoSuggestionIsANoop(t *testing.T) {
	a, f := testCoreApp(t)
	if cmd := a.respondRuleSuggestion(true); cmd != nil {
		execCmdTree(cmd)
	}
	if len(f.ruleSuggestionAnswers) != 0 {
		t.Errorf("must not call the RPC with no pending suggestion, got %+v", f.ruleSuggestionAnswers)
	}
}

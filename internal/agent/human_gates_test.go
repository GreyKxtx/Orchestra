package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

type scriptedAsker struct {
	answer string
	asked  []tools.QuestionItem
}

func (s *scriptedAsker) Ask(_ context.Context, qs []tools.QuestionItem) ([]string, error) {
	s.asked = append(s.asked, qs...)
	return []string{s.answer}, nil
}

func TestConfirmHumanGate_ApproveAndDecline(t *testing.T) {
	asker := &scriptedAsker{answer: "yes"}
	a := &Agent{opts: Options{
		HumanGates:    map[string]bool{"git_push": true},
		QuestionAsker: asker,
	}}

	// Non-gated tool passes without asking.
	if err := a.confirmHumanGate(context.Background(), "read", nil); err != nil {
		t.Fatalf("read must not be gated: %v", err)
	}
	if len(asker.asked) != 0 {
		t.Fatal("non-gated tool must not trigger a question")
	}

	// git.commit gate is off in config → passes silently.
	if err := a.confirmHumanGate(context.Background(), "git.commit", []byte(`{"message":"m"}`)); err != nil {
		t.Fatalf("git.commit gate is off: %v", err)
	}

	// git.push gate required → asks; "yes" approves.
	if err := a.confirmHumanGate(context.Background(), "git.push", []byte(`{"remote":"origin","branch":"main"}`)); err != nil {
		t.Fatalf("approved push must pass: %v", err)
	}
	if len(asker.asked) != 1 || !strings.Contains(asker.asked[0].Question, "origin/main") {
		t.Fatalf("expected one G3 question mentioning origin/main, got %+v", asker.asked)
	}

	// Decline blocks the call.
	asker.answer = "no"
	if err := a.confirmHumanGate(context.Background(), "git.push", []byte(`{}`)); err == nil {
		t.Fatal("declined push must be denied")
	}
}

func TestConfirmHumanGate_FailClosedWithoutAsker(t *testing.T) {
	a := &Agent{opts: Options{HumanGates: map[string]bool{"git_commit": true}}}
	err := a.confirmHumanGate(context.Background(), "git.commit", []byte(`{"message":"feat: x"}`))
	if err == nil {
		t.Fatal("required gate without QuestionAsker must deny (fail-closed)")
	}
	if !strings.Contains(err.Error(), "unblock") {
		t.Fatalf("gate error must carry an unblock path, got: %v", err)
	}
}

func TestIsAffirmativeAnswer(t *testing.T) {
	for _, yes := range []string{"yes", "Y", " да ", "1", "OK", "approve"} {
		if !isAffirmativeAnswer(yes) {
			t.Errorf("%q must be affirmative", yes)
		}
	}
	for _, no := range []string{"", "no", "нет", "2", "maybe", "yes but later"} {
		if isAffirmativeAnswer(no) {
			t.Errorf("%q must be a decline", no)
		}
	}
}

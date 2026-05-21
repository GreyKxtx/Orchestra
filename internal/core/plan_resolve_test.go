package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/core/session"
)

func TestSessionPlanPathLocked_assignsOnce(t *testing.T) {
	s := session.New()
	if got := sessionPlanPathLocked(s, "build"); got != "" {
		t.Fatalf("build mode: got %q, want empty", got)
	}
	first := sessionPlanPathLocked(s, "plan")
	if first == "" {
		t.Fatal("plan mode: expected non-empty path")
	}
	if want := ".orchestra/plans/" + s.ID + ".md"; first != want {
		t.Fatalf("first = %q, want %q", first, want)
	}
	second := sessionPlanPathLocked(s, "plan")
	if second != first {
		t.Fatalf("second = %q, want stable %q", second, first)
	}
}

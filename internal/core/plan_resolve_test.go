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

func TestSessionPlanPathLocked_architecture(t *testing.T) {
	s := session.New()
	p := sessionPlanPathLocked(s, "architecture")
	if p == "" {
		t.Fatal("architecture should get a plan path")
	}
	if got := sessionPlanPathLocked(s, "ask"); got != p {
		// already set on session — stable regardless of mode
		t.Fatalf("stable path: got %q want %q", got, p)
	}
}

func TestResolvePlanPath_modes(t *testing.T) {
	if resolvePlanPath("ask", "", "") != "" {
		t.Fatal("ask must not allocate plan path")
	}
	if resolvePlanPath("architecture", "", "sid") == "" {
		t.Fatal("architecture should allocate plan path")
	}
	if resolvePlanPath("orchestra", "", "sid") == "" {
		t.Fatal("orchestra should allocate plan path")
	}
}

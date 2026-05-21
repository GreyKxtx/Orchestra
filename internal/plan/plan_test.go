package plan

import "testing"

func TestSessionRelPath(t *testing.T) {
	got := SessionRelPath("abc123")
	want := ".orchestra/plans/abc123.md"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsWritablePath(t *testing.T) {
	assigned := ".orchestra/plans/sess1.md"
	cases := []struct {
		path string
		ok   bool
	}{
		{assigned, true},
		{".orchestra/plan.md", true},
		{".orchestra/plans/other.md", true},
		{"src/main.go", false},
		{".orchestra/memory/agent.md", false},
	}
	for _, tc := range cases {
		if got := IsWritablePath(tc.path, assigned); got != tc.ok {
			t.Errorf("IsWritablePath(%q) = %v, want %v", tc.path, got, tc.ok)
		}
	}
}

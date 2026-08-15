package lessons

import "testing"

func TestBumpAntiPatternSignal(t *testing.T) {
	root := t.TempDir()
	key := "verification_failed: go test ./pkg"
	for i := 0; i < PromoteSuggestThreshold; i++ {
		if got := BumpAntiPatternSignal(root, "frontend", key); got != i+1 {
			t.Fatalf("bump %d = %d", i+1, got)
		}
	}
	if hint := FormatPromoteHint("frontend", PromoteSuggestThreshold); hint == "" || !contains(hint, "lesson_promote") {
		t.Fatalf("hint=%q", hint)
	}
	ClearAntiPatternSignals(root, "frontend")
	if got := BumpAntiPatternSignal(root, "frontend", key); got != 1 {
		t.Fatalf("after clear = %d", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

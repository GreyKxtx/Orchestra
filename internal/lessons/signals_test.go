package lessons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// A signal log is trimmed to its tail past the cap: the count that matters
// is recent repeats, and the file was appended forever.
func TestBumpAntiPatternSignal_TrimsTheLogToItsTail(t *testing.T) {
	oldMax, oldKeep := maxSignalLines, keepSignalLines
	maxSignalLines, keepSignalLines = 5, 3
	t.Cleanup(func() { maxSignalLines, keepSignalLines = oldMax, oldKeep })
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		BumpAntiPatternSignal(root, "frontend", "k"+string(rune('a'+i)))
	}
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "lessons", "signals", "frontend.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("log has %d lines after the trim, want 3:\n%s", len(lines), data)
	}
	if !strings.HasSuffix(lines[2], "|kf") || !strings.HasSuffix(lines[0], "|kd") {
		t.Fatalf("the newest lines must be the ones kept:\n%s", data)
	}
	// The trimmed-away key no longer counts as a repeat.
	if got := BumpAntiPatternSignal(root, "frontend", "ka"); got != 1 {
		t.Fatalf("count of a trimmed key = %d, want 1", got)
	}
}

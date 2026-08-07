package memory

import (
	"strings"
	"testing"
)

func TestPinnedEntriesAndSearch(t *testing.T) {
	raw := "---\n[pin] Always use gofmt\n---\n2026 note about widgets\n---\nother stuff\n"
	pins := PinnedEntries(raw)
	if len(pins) != 1 || !IsPinnedEntry(pins[0]) {
		t.Fatalf("pins=%v", pins)
	}
	hits := SearchEntries(raw, "gofmt", 5)
	if len(hits) != 1 {
		t.Fatalf("search hits=%v", hits)
	}
	joined := joinEntriesRecentFirst(splitEntries(raw))
	if !strings.Contains(joined, "[pin]") {
		t.Fatalf("expected pin in inject: %q", joined)
	}
	// Pin should appear before the non-pin "widgets" note when recent-first.
	pinAt := strings.Index(joined, "[pin]")
	widgetAt := strings.Index(joined, "widgets")
	if pinAt < 0 || widgetAt < 0 || pinAt > widgetAt {
		t.Fatalf("pin should come before other entries: %q", joined)
	}
}

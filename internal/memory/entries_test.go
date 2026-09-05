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

func TestSplitEntries_FirstEntryDoesNotKeepTheLeadingSeparator(t *testing.T) {
	// Every entry is written as "\n---\n<body>", so the file begins with a
	// separator. TrimSpace turns that leading "\n---\n" into "---\n", which
	// Split can no longer cut — the first stored fact has always carried a
	// stray "---" into search hits, memory_read output and injected text.
	raw := "\n---\n*t1*\n\nfirst\n\n---\n*t2*\n\nsecond\n"
	got := splitEntries(raw)
	if len(got) != 2 {
		t.Fatalf("entries = %d (%q), want 2", len(got), got)
	}
	if strings.HasPrefix(got[0], "---") {
		t.Errorf("first entry = %q, want no leading separator", got[0])
	}
	if !strings.HasPrefix(got[0], "*t1*") {
		t.Errorf("first entry = %q", got[0])
	}
}

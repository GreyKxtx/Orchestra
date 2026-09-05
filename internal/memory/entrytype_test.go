package memory

import (
	"strings"
	"testing"
)

func TestNormalizeEntryType(t *testing.T) {
	cases := map[string]string{
		"feedback":  TypeFeedback,
		"FEEDBACK":  TypeFeedback,
		" user ":    TypeUser,
		"project":   TypeProject,
		"reference": TypeReference,
		// Anything unrecognised, including empty, is a project fact: that is
		// what an untyped note has always been.
		"":       TypeProject,
		"bogus":  TypeProject,
		"pinned": TypeProject,
	}
	for in, want := range cases {
		if got := NormalizeEntryType(in); got != want {
			t.Errorf("NormalizeEntryType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntryTypeOf(t *testing.T) {
	cases := map[string]string{
		"*2026-09-05T10:00:00Z* [feedback]\n\nDo not reformat files you did not edit.": TypeFeedback,
		"*2026-09-05T10:00:00Z* [user]\n\nPrefers Russian.":                            TypeUser,
		"*2026-09-05T10:00:00Z* [reference]\n\nDesign doc: docs/x.md":                   TypeReference,
		"*2026-09-05T10:00:00Z* [project]\n\nBuild runs via make.":                      TypeProject,
		// Written before types existed — still a project fact, not a parse error.
		"*2026-09-05T10:00:00Z*\n\nBuild runs via make.": TypeProject,
		// A [pin] marker is orthogonal and must not be read as a type.
		"*2026-09-05T10:00:00Z* [feedback]\n\n[pin] Always run gofmt.": TypeFeedback,
		"[pin] Always run gofmt.":                                     TypeProject,
	}
	for entry, want := range cases {
		if got := EntryTypeOf(entry); got != want {
			t.Errorf("EntryTypeOf(%q) = %q, want %q", preview(entry, 40), got, want)
		}
	}
}

func TestFormatEntry_RoundTrips(t *testing.T) {
	e := formatEntry("2026-09-05T10:00:00Z", TypeFeedback, "Do not reformat untouched files.")
	if !strings.Contains(e, "[feedback]") {
		t.Fatalf("entry = %q", e)
	}
	if got := EntryTypeOf(strings.TrimSpace(strings.TrimPrefix(e, entrySep))); got != TypeFeedback {
		t.Errorf("round trip = %q", got)
	}
	if !strings.Contains(e, "Do not reformat untouched files.") {
		t.Errorf("entry lost its content: %q", e)
	}
}

func TestJoinEntriesByPriority(t *testing.T) {
	// Written oldest-first, as the file holds them.
	entries := []string{
		"*t1* [project]\n\nproject-old",
		"*t2* [reference]\n\nreference",
		"*t3* [feedback]\n\nfeedback-old",
		"*t4*\n\nuntyped-legacy",
		"*t5* [user]\n\nuser",
		"*t6* [feedback]\n\nfeedback-new",
		"*t7* [project]\n\n[pin] pinned-project",
	}
	got := joinEntriesByPriority(entries)

	// The order that matters: a pin outranks everything, then feedback (the
	// most expensive thing to lose), then user, then project, then reference.
	want := []string{
		"pinned-project",
		"feedback-new", "feedback-old",
		"user",
		// recent first inside the type: t4 is newer than t1
		"untyped-legacy", "project-old",
		"reference",
	}
	var positions []int
	for _, w := range want {
		i := strings.Index(got, w)
		if i < 0 {
			t.Fatalf("%q is missing from the join:\n%s", w, got)
		}
		positions = append(positions, i)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] < positions[i-1] {
			t.Fatalf("%q came before %q:\n%s", want[i], want[i-1], got)
		}
	}
}

func TestJoinEntriesByPriority_RecentFirstWithinAType(t *testing.T) {
	entries := []string{
		"*t1* [project]\n\noldest",
		"*t2* [project]\n\nmiddle",
		"*t3* [project]\n\nnewest",
	}
	got := joinEntriesByPriority(entries)
	if strings.Index(got, "newest") > strings.Index(got, "oldest") {
		t.Errorf("within one type the newest entry must come first:\n%s", got)
	}
}

func TestJoinEntriesByPriority_Empty(t *testing.T) {
	if got := joinEntriesByPriority(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

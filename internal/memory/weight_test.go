package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// entryAt renders a stored entry the way the file holds it, dated `age` ago.
func entryAt(age time.Duration, entryType, content string) string {
	ts := time.Now().UTC().Add(-age).Format("2006-01-02T15:04:05Z")
	return fmt.Sprintf("*%s* [%s]\n\n%s", ts, entryType, content)
}

// indexOfContent returns where an entry with this content landed in the order.
func indexOfContent(ordered []string, content string) int {
	for i, e := range ordered {
		if strings.Contains(e, content) {
			return i
		}
	}
	return -1
}

// Types alone decided the order: a correction from two years ago outranked a
// fact learned yesterday, forever. Both dimensions now count.
func TestOrderEntriesByPriority_FreshFeedbackStillBeatsOldProject(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		entryAt(180*24*time.Hour, TypeProject, "OLDPROJECT"),
		entryAt(20*24*time.Hour, TypeFeedback, "FRESHFEEDBACK"),
	})
	if i, j := indexOfContent(ordered, "FRESHFEEDBACK"), indexOfContent(ordered, "OLDPROJECT"); i > j {
		t.Errorf("a month-old correction ranked below a six-month-old project fact:\n%v", ordered)
	}
}

// The new half of the rule, and the whole point of weighting rather than
// sorting: a correction old enough to have stopped being true must yield to a
// fact the project just learned.
func TestOrderEntriesByPriority_StaleFeedbackYieldsToFreshProject(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		entryAt(2*365*24*time.Hour, TypeFeedback, "ANCIENTFEEDBACK"),
		entryAt(time.Hour, TypeProject, "TODAYSPROJECT"),
	})
	if i, j := indexOfContent(ordered, "TODAYSPROJECT"), indexOfContent(ordered, "ANCIENTFEEDBACK"); i > j {
		t.Errorf("a two-year-old correction still outranked a fact learned today:\n%v", ordered)
	}
}

// Type still dominates at comparable ages — that was the field-run finding
// (§1.2 #3), and freshness is a modifier on it, not a replacement for it.
func TestOrderEntriesByPriority_TypeStillWinsAtComparableAge(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		entryAt(24*time.Hour, TypeProject, "YESTERDAYPROJECT"),
		entryAt(48*time.Hour, TypeFeedback, "TWODAYFEEDBACK"),
	})
	if i, j := indexOfContent(ordered, "TWODAYFEEDBACK"), indexOfContent(ordered, "YESTERDAYPROJECT"); i > j {
		t.Errorf("a one-day age gap flipped the type order:\n%v", ordered)
	}
}

// Pinned means "never lose this". Age must not touch it.
func TestOrderEntriesByPriority_PinsStayFirstAtAnyAge(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		entryAt(time.Hour, TypeFeedback, "FRESHFEEDBACK"),
		entryAt(5*365*24*time.Hour, TypeReference, "[pin]\nANCIENTPIN"),
	})
	if indexOfContent(ordered, "ANCIENTPIN") != 0 {
		t.Errorf("a five-year-old pin was not first:\n%v", ordered)
	}
}

func TestOrderEntriesByPriority_RecentFirstWithinAType(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		entryAt(72*time.Hour, TypeProject, "OLDER"),
		entryAt(1*time.Hour, TypeProject, "NEWER"),
	})
	if i, j := indexOfContent(ordered, "NEWER"), indexOfContent(ordered, "OLDER"); i > j {
		t.Errorf("older entry came first within one type:\n%v", ordered)
	}
}

// Every note written before types and timestamps existed has no parsable date.
// Treating that as "infinitely old" would bury the entire pre-existing file
// below anything written since; treating it as "brand new" would let it
// outrank real corrections. It reads as neither: no freshness bonus and no
// penalty, so it keeps its type's place.
func TestOrderEntriesByPriority_UndatedEntriesKeepTheirTypesPlace(t *testing.T) {
	ordered := orderEntriesByPriority([]string{
		"no timestamp at all, just text",
		entryAt(time.Hour, TypeReference, "FRESHREFERENCE"),
	})
	// An undated entry reads as a project fact (EntryTypeOf's default), which
	// outranks a reference regardless of the reference being newer.
	if i, j := indexOfContent(ordered, "no timestamp"), indexOfContent(ordered, "FRESHREFERENCE"); i > j {
		t.Errorf("an undated project note sank below a fresh reference:\n%v", ordered)
	}
}

func TestEntryTimestamp(t *testing.T) {
	want := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	got, ok := entryTimestamp("*2026-09-01T10:00:00Z* [feedback]\n\nbody")
	if !ok || !got.Equal(want) {
		t.Errorf("entryTimestamp = %v/%v, want %v/true", got, ok, want)
	}
	if _, ok := entryTimestamp("no marker here"); ok {
		t.Error("a line with no timestamp reported one")
	}
	// A [pin] in the body must not be mistaken for the header.
	if _, ok := entryTimestamp("[pin]\nsome text"); ok {
		t.Error("a pinned body with no timestamp reported one")
	}
}

// Compaction deletes. Injection only chooses what to show this turn, so
// demoting a stale correction there costs nothing and can be undone by the
// next turn; dropping it from disk cannot. Compaction therefore keeps the
// strict type order, and this asymmetry is deliberate.
func TestSelectEntriesToKeep_CompactionIgnoresFreshness(t *testing.T) {
	ancientFeedback := entryAt(3*365*24*time.Hour, TypeFeedback, "ANCIENTFEEDBACK")
	freshProject := entryAt(time.Hour, TypeProject, "TODAYSPROJECT")

	// Room for exactly one of them.
	budget := len(ancientFeedback) + len(entrySep) + 1
	kept := selectEntriesToKeep([]string{ancientFeedback, freshProject}, budget)

	if !kept[0] {
		t.Error("compaction deleted a correction because it was old; injection may " +
			"demote it, but disk is forever")
	}
	if kept[1] {
		t.Error("both entries were kept; the budget was supposed to fit only one")
	}
}

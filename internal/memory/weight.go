package memory

import (
	"math"
	"strings"
	"time"
)

// Injection ranks entries by type AND age, not by type alone.
//
// Types alone (the previous rule) meant a correction from two years ago
// outranked a fact the project learned yesterday, forever — and a correction
// old enough to have stopped being true is not worth the budget it takes from
// one that is still current. Age alone was the rule before types existed, and
// it buried week-one feedback under a hundred "read file X" notes.
//
// The weight is typeWeight × recency, so the two dimensions trade off:
//
//	fresh feedback      4 × 1.00 = 4.00
//	feedback, 1 month   4 × 0.79 = 3.17   still above any fresh project fact
//	feedback, 3 months  4 × 0.50 = 2.00   level with a fact learned today
//	feedback, 2 years   4 × 0.25 = 1.00   below it — the point of weighting
//	fresh project       2 × 1.00 = 2.00
//	project, 2 years    2 × 0.25 = 0.50
//	fresh reference     1 × 1.00 = 1.00
//
// Type still dominates at comparable ages, which is the field-run finding
// §1.2 #3 rests on; freshness is a modifier on that, not a replacement.
const (
	// recencyHalfLife is how long it takes an entry to lose half its
	// freshness. Ninety days is roughly a project quarter: long enough that
	// nothing decays inside one piece of work, short enough that a year-old
	// correction stops outranking current facts.
	recencyHalfLife = 90 * 24 * time.Hour
	// recencyFloor keeps age from erasing an entry entirely. Without it a
	// sufficiently old feedback would sort below a fresh reference — and the
	// reason a fact is old is often that it has been true and unchallenged
	// the whole time.
	recencyFloor = 0.25
)

// typeWeight is the base value of an entry by type, in the order §1.2 #3
// established: losing feedback is most expensive, a reference is cheapest to
// re-find. The gaps are wide enough that only a multi-month age difference
// crosses one.
func typeWeight(entryType string) float64 {
	switch entryType {
	case TypeFeedback:
		return 4
	case TypeUser:
		return 3
	case TypeReference:
		return 1
	default: // TypeProject, and every untyped note ever written
		return 2
	}
}

// recencyFactor decays from 1 to recencyFloor with a half-life of
// recencyHalfLife. An entry with no parsable timestamp scores 1: every note
// written before timestamps existed would otherwise read as infinitely old and
// the whole pre-existing file would sink below anything written since.
func recencyFactor(age time.Duration) float64 {
	if age <= 0 {
		return 1
	}
	f := math.Pow(2, -age.Hours()/recencyHalfLife.Hours())
	if f < recencyFloor {
		return recencyFloor
	}
	return f
}

// entryScore is the injection weight of one entry. Higher sorts earlier.
func entryScore(entry string, now time.Time) float64 {
	w := typeWeight(EntryTypeOf(entry))
	ts, ok := entryTimestamp(entry)
	if !ok {
		return w
	}
	return w * recencyFactor(now.Sub(ts))
}

// entryTimestamp reads the write time from an entry's header line —
// "*2026-09-01T10:00:00Z* [feedback]".
//
// Only the first line is examined, and only between the first pair of
// asterisks, so a date written inside the body is never mistaken for the
// entry's own.
func entryTimestamp(entry string) (time.Time, bool) {
	line, _, _ := strings.Cut(strings.TrimSpace(entry), "\n")
	open := strings.Index(line, "*")
	if open < 0 {
		return time.Time{}, false
	}
	rest := line[open+1:]
	close := strings.Index(rest, "*")
	if close < 0 {
		return time.Time{}, false
	}
	ts, err := time.Parse("2006-01-02T15:04:05Z", strings.TrimSpace(rest[:close]))
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

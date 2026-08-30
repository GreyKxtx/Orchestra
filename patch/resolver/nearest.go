package resolver

import (
	"fmt"
	"strings"
)

const (
	nearestContextBefore = 6
	nearestContextAfter  = 10
	nearestMaxBytes      = 1200
	nearestMaxLineChars  = 200
)

// nearestMinSimilarity keeps the hint honest: below this the "closest" line is
// noise, and pointing at it would send the model to the wrong part of the file.
const nearestMinSimilarity = 0.5

// NearestRegionHint returns a numbered excerpt of the part of content that most
// resembles the search block, or "" when nothing in the file resembles it.
//
// A failed search/replace otherwise tells the model only that its block was not
// found, so its cheapest recovery is to re-read the whole file and try again —
// two extra steps, and a whole file back into the history. Handing it the
// actual current text around the place it was aiming at usually lets it fix the
// search block in the very next call. This matters most after a read has been
// digested out of history, when the model is reconstructing the block from
// memory.
func NearestRegionHint(content []byte, search string) string {
	anchor := firstMeaningfulLine(search)
	if anchor == "" {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return ""
	}

	// Reuse the resolver's own line similarity (Levenshtein ratio) so the hint
	// points at the same place the forgiving matcher would have aimed.
	best, bestScore := -1, 0.0
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if s := lineSimilarity(t, anchor); s > bestScore {
			best, bestScore = i, s
		}
	}
	if best < 0 || bestScore < nearestMinSimilarity {
		return ""
	}

	from := best - nearestContextBefore
	if from < 0 {
		from = 0
	}
	to := best + nearestContextAfter
	if to > len(lines)-1 {
		to = len(lines) - 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "file has %d lines; current text around line %d:\n", len(lines), best+1)
	for i := from; i <= to; i++ {
		ln := lines[i]
		if len(ln) > nearestMaxLineChars {
			ln = ln[:nearestMaxLineChars] + "…"
		}
		fmt.Fprintf(&b, "%6d| %s\n", i+1, ln)
		if b.Len() > nearestMaxBytes {
			b.WriteString("       | …\n")
			break
		}
	}
	return b.String()
}

// firstMeaningfulLine returns the first non-blank, non-trivial line of s.
// Lines like "}" or ")" anchor nothing — they match everywhere.
func firstMeaningfulLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if len(t) >= 3 {
			return t
		}
	}
	return ""
}

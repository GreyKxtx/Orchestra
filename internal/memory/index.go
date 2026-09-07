package memory

import (
	"sort"
	"strings"
	"time"
)

// indexLineMax caps one index row. Long enough to recognise a fact, short
// enough that a hundred of them still cost less than a handful of full
// entries.
const indexLineMax = 110

// indexHeader introduces the index block. It tells the model both that more
// memory exists and how to get at it — an index it cannot act on is just
// noise in the prompt.
const indexHeader = "[memory index — not shown in full; read with memory_read layer=repo]"

// entryIndexLine renders one entry as a single index row: its type and the
// opening of its body.
func entryIndexLine(entry string) string {
	body := entryBody(entry)
	first, _, _ := strings.Cut(body, "\n")
	first = strings.TrimSpace(first)
	if len(first) > indexLineMax {
		first = strings.TrimSpace(first[:indexLineMax]) + "…"
	}
	return "- [" + EntryTypeOf(entry) + "] " + first
}

// sliceEntriesWithIndex renders entries into a prompt budget: as many as fit
// in full, in priority order, and a one-line index for the rest.
//
// Before this, entries past the budget were simply cut off. The model had no
// way to know they existed, so it could not ask for them either — memory it
// could not see was memory it could not use. An index costs about one line
// per fact and turns the overflow into something addressable.
func sliceEntriesWithIndex(entries []string, maxBytes int) string {
	if len(entries) == 0 || maxBytes <= 0 {
		return ""
	}
	ordered := orderEntriesByPriority(entries)

	var full []string
	var overflow []string
	used := 0
	for _, e := range ordered {
		size := len(e) + len(entrySep) + 1
		// Reserve room for the overflow index as soon as anything overflows.
		if used+size > maxBytes || len(overflow) > 0 {
			overflow = append(overflow, e)
			continue
		}
		full = append(full, e)
		used += size
	}
	if len(overflow) == 0 {
		return strings.Join(full, entrySep+"\n")
	}

	// Give the index whatever is left, and if nothing is left take it from
	// the least important entry that did fit: knowing a fact exists beats
	// having one more full entry the model may not need.
	var idx []string
	budget := maxBytes - used
	for _, e := range overflow {
		line := entryIndexLine(e)
		if budget-len(line)-1 < 0 && len(full) > 1 {
			last := full[len(full)-1]
			full = full[:len(full)-1]
			budget += len(last) + len(entrySep) + 1
		}
		if budget-len(line)-1 < 0 {
			break
		}
		idx = append(idx, line)
		budget -= len(line) + 1
	}
	if len(idx) == 0 {
		return strings.Join(full, entrySep+"\n")
	}

	block := indexHeader + "\n" + strings.Join(idx, "\n")
	if len(full) == 0 {
		return block
	}
	return strings.Join(full, entrySep+"\n") + "\n\n" + block
}

// orderEntriesByPriority is joinEntriesByPriority's ordering, kept as a slice
// so the caller can measure each entry rather than one joined string.
func orderEntriesByPriority(entries []string) []string {
	now := time.Now().UTC()

	var pins []string
	var rest []string
	for _, e := range entries {
		if IsPinnedEntry(e) {
			pins = append(pins, e)
			continue
		}
		rest = append(rest, e)
	}

	// Pins are unconditional and keep their own recency order: "never lose
	// this" is not a claim that ages.
	out := make([]string, 0, len(entries))
	out = append(out, reverseEntries(pins)...)

	// Everything else is ranked by entryScore — type weighted by freshness.
	// entries arrive oldest-first, so reversing before the stable sort makes
	// recency the tie-breaker for entries of equal score.
	rest = reverseEntries(rest)
	scores := make(map[int]float64, len(rest))
	for i, e := range rest {
		scores[i] = entryScore(e, now)
	}
	idx := make([]int, len(rest))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	for _, i := range idx {
		out = append(out, rest[i])
	}
	return out
}

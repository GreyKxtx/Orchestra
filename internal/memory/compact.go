package memory

import (
	"os"
	"strings"
)

func (s *Store) compactAgentFile(path string) error {
	maxBytes := s.cfg.MaxAgentBytes()
	if maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= int64(maxBytes) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entries := splitEntries(string(data))
	if len(entries) <= 1 {
		trimmed := tailBytes(string(data), maxBytes)
		return os.WriteFile(path, []byte(trimmed), 0644)
	}

	kept := selectEntriesToKeep(entries, maxBytes)

	// Restore file order (oldest first) among what survived: the file is a
	// log a person reads, even though injection re-sorts it by type.
	var final []string
	for i, e := range entries {
		if kept[i] {
			final = append(final, e)
		}
	}
	body := strings.Join(final, entrySep)
	if !strings.HasPrefix(body, "---") {
		body = entrySep + body
	}
	return os.WriteFile(path, []byte(body+"\n"), 0644)
}

// selectEntriesToKeep decides what survives compaction, in the priority order
// injection uses: pinned first, then feedback, user, project, reference, and
// within each type the most recent first.
//
// Trimming by recency alone was the gap that made typing pointless over a
// long project: a correction from week one is dropped from the file before
// the injection ordering ever gets to protect it, and the user has to give it
// again. Recency still decides *within* a type, which is where it belongs.
// The result is indexed by position in entries, not keyed by the entry text:
// two notes can be byte-identical (the same fact written twice is exactly
// what deduplication has to deal with), and a text-keyed set collapses them
// into one, which then marks every copy as kept and compacts nothing.
func selectEntriesToKeep(entries []string, maxBytes int) []bool {
	var pins []int
	byType := map[string][]int{}
	for i, e := range entries {
		if IsPinnedEntry(e) {
			pins = append(pins, i)
			continue
		}
		t := EntryTypeOf(e)
		byType[t] = append(byType[t], i)
	}

	// Pins are unconditional, as before: surviving compaction is what they
	// are for, even if they alone exceed the budget.
	kept := make([]bool, len(entries))
	total := 0
	for _, i := range pins {
		kept[i] = true
		total += len(entries[i]) + len(entrySep)
	}

	for _, t := range injectionOrder {
		idx := byType[t]
		for n := len(idx) - 1; n >= 0; n-- { // most recent first within the type
			i := idx[n]
			size := len(entries[i]) + len(entrySep)
			if total+size > maxBytes {
				// Too big for what is left. Keep going rather than stop: a
				// shorter entry of the same or a lower type may still fit,
				// and giving up here would waste the remaining budget.
				continue
			}
			kept[i] = true
			total += size
		}
	}
	return kept
}

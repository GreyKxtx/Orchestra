package memory

import (
	"os"
	"strings"
	"unicode"
)

// dedupThreshold is the Jaccard overlap at which two notes are treated as the
// same fact restated. Deliberately high: merging two genuinely different
// facts loses one of them silently, while failing to merge only leaves a
// duplicate that compaction will handle.
const dedupThreshold = 0.6

// entrySimilarity scores two notes on token overlap (Jaccard).
//
// No embeddings: this runs on every memory_write, including on a local model
// with no embedding endpoint, and the job here is only to notice a fact being
// restated. Semantic search over memory is a separate path (semantic.go).
func entrySimilarity(a, b string) float64 {
	ta, tb := contentTokens(a), contentTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for tok := range ta {
		if tb[tok] {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// contentTokens reduces an entry to a set of comparable words, dropping the
// "*<ts>* [type]" header so two unrelated facts do not look alike merely for
// sharing one.
func contentTokens(entry string) map[string]bool {
	body := entryBody(entry)
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(body), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 2 || stopWords[f] {
			continue
		}
		out[f] = true
	}
	return out
}

// entryBody strips the header line from a stored entry. Content written
// directly by a caller has no header and passes through unchanged.
func entryBody(entry string) string {
	s := strings.TrimSpace(entry)
	line, rest, found := strings.Cut(s, "\n")
	if found && strings.HasPrefix(strings.TrimSpace(line), "*") {
		return strings.TrimSpace(rest)
	}
	return s
}

// stopWords are the words too common to say anything about whether two notes
// mean the same thing.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "to": true, "of": true, "in": true, "on": true,
	"at": true, "for": true, "and": true, "or": true, "not": true, "it": true,
	"this": true, "that": true, "with": true, "by": true, "as": true,
	"pin": true,
}

// findNearDuplicate returns the index of the entry that restates content, if
// any. Entries arrive in file order; the most recent match wins, since that
// is the one whose wording the user last saw.
func findNearDuplicate(entries []string, content string) (int, bool) {
	best, bestScore := -1, dedupThreshold
	for i := len(entries) - 1; i >= 0; i-- {
		if score := entrySimilarity(entries[i], content); score >= bestScore {
			best, bestScore = i, score
		}
	}
	return best, best >= 0
}

// replaceNearDuplicate rewrites an existing entry that restates content,
// returning whether it did. The new wording, timestamp and type win: the
// caller is saying this is how the fact stands now.
//
// A [pin] on the old entry is carried over. A pin is the user saying "never
// lose this", and restating the fact is not a request to drop that.
func (s *Store) replaceNearDuplicate(path, content, newEntry string) (bool, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0, nil // no file yet: nothing to replace
	}
	entries := splitEntries(string(data))
	i, ok := findNearDuplicate(entries, content)
	if !ok {
		return false, 0, nil
	}

	replacement := strings.TrimPrefix(newEntry, entrySep)
	if IsPinnedEntry(entries[i]) && !IsPinnedEntry(replacement) {
		replacement = pinReplacementBody(replacement)
	}
	entries[i] = strings.TrimSpace(replacement)

	body := entrySep + strings.Join(entries, entrySep) + "\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return false, 0, err
	}
	return true, len(replacement), nil
}

// pinReplacementBody re-applies a [pin] marker to the body of an entry,
// leaving the "*<ts>* [type]" header alone.
func pinReplacementBody(entry string) string {
	header, body, found := strings.Cut(entry, "\n\n")
	if !found {
		return "[pin] " + entry
	}
	return header + "\n\n[pin] " + strings.TrimSpace(body)
}

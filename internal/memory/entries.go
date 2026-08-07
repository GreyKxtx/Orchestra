package memory

import (
	"strings"
)

const entrySep = "\n---\n"

// splitEntries splits markdown files that use --- delimiters (memory_write format).
// Returns entries in file order (oldest first).
func splitEntries(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	parts := strings.Split(content, entrySep)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinEntriesRecentFirst concatenates entries with most recent first (for inject).
// Pinned entries ([pin] / pin:) always come first.
func joinEntriesRecentFirst(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	var pins, rest []string
	for _, e := range entries {
		if IsPinnedEntry(e) {
			pins = append(pins, e)
		} else {
			rest = append(rest, e)
		}
	}
	reversed := make([]string, len(rest))
	for i, e := range rest {
		reversed[len(rest)-1-i] = e
	}
	out := append(append([]string{}, pins...), reversed...)
	return strings.Join(out, entrySep+"\n")
}

// IsPinnedEntry reports sticky facts that must survive agent.md compaction.
func IsPinnedEntry(entry string) bool {
	s := strings.TrimSpace(entry)
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "[pin]") ||
		strings.HasPrefix(lower, "pin:") ||
		strings.Contains(lower, "\n[pin]")
}

// PinnedEntries returns pin-marked entries from agent.md-style content.
func PinnedEntries(content string) []string {
	var out []string
	for _, e := range splitEntries(content) {
		if IsPinnedEntry(e) {
			out = append(out, e)
		}
	}
	return out
}

// SearchEntries does case-insensitive substring search across entries (Phase 2 memory_search).
func SearchEntries(content, query string, limit int) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	var out []string
	for _, e := range splitEntries(content) {
		if strings.Contains(strings.ToLower(e), query) {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// tailBytes keeps the last maxBytes of s, preferring entry boundaries when possible.
func tailBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	tail := s[len(s)-maxBytes:]
	if idx := strings.Index(tail, entrySep); idx >= 0 {
		tail = tail[idx+len(entrySep):]
	}
	return strings.TrimSpace(tail)
}

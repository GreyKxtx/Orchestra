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
func joinEntriesRecentFirst(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	reversed := make([]string, len(entries))
	for i, e := range entries {
		reversed[len(entries)-1-i] = e
	}
	return strings.Join(reversed, entrySep+"\n")
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

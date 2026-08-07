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
	// Always keep pinned facts; fill remaining budget with recent entries.
	var pins, rest []string
	for _, e := range entries {
		if IsPinnedEntry(e) {
			pins = append(pins, e)
		} else {
			rest = append(rest, e)
		}
	}
	var kept []string
	total := 0
	for _, e := range pins {
		add := len(e) + len(entrySep)
		kept = append(kept, e)
		total += add
	}
	for i := len(rest) - 1; i >= 0; i-- {
		e := rest[i]
		add := len(e) + len(entrySep)
		if total+add > maxBytes && len(kept) > len(pins) {
			break
		}
		if total+add > maxBytes && len(kept) == len(pins) {
			// Still over budget with pins only — keep pins anyway.
			break
		}
		kept = append(kept, e)
		total += add
		// Insert recent at end; we'll reverse non-pin portion for file order (oldest→newest).
	}
	// Rebuild: pins first (stable), then rest chronologically (oldest first among kept rest).
	var restKept []string
	for _, e := range kept {
		if !IsPinnedEntry(e) {
			restKept = append(restKept, e)
		}
	}
	// restKept was appended newest-first; reverse to oldest-first for file.
	for i, j := 0, len(restKept)-1; i < j; i, j = i+1, j-1 {
		restKept[i], restKept[j] = restKept[j], restKept[i]
	}
	final := append(append([]string{}, pins...), restKept...)
	body := strings.Join(final, entrySep)
	if !strings.HasPrefix(body, "---") {
		body = entrySep + body
	}
	return os.WriteFile(path, []byte(body+"\n"), 0644)
}

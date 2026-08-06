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
	// Keep recent entries until under budget.
	var kept []string
	total := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		add := len(e) + len(entrySep)
		if total+add > maxBytes && len(kept) > 0 {
			break
		}
		kept = append([]string{e}, kept...)
		total += add
	}
	body := strings.Join(kept, entrySep)
	if !strings.HasPrefix(body, "---") {
		body = entrySep + body
	}
	return os.WriteFile(path, []byte(body+"\n"), 0644)
}


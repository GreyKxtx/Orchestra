package lessons

import (
	"os"
	"path/filepath"
	"strings"
)

// SearchHit is one lessons search match.
type SearchHit struct {
	Dept    string
	Snippet string
}

// Search scans .orchestra/memory/lessons/*.md for a case-insensitive substring.
func Search(projectRoot, query string, limit int) []SearchHit {
	query = strings.TrimSpace(strings.ToLower(query))
	if projectRoot == "" || query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	dir := filepath.Join(projectRoot, filepath.FromSlash(RelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var hits []SearchHit
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		dept := strings.TrimSuffix(ent.Name(), ".md")
		for _, block := range strings.Split(string(data), "\n## ") {
			block = strings.TrimSpace(block)
			if block == "" {
				continue
			}
			if !strings.Contains(strings.ToLower(block), query) {
				continue
			}
			snip := block
			if len(snip) > 400 {
				snip = snip[:400] + "…"
			}
			hits = append(hits, SearchHit{Dept: dept, Snippet: snip})
			if len(hits) >= limit {
				return hits
			}
		}
	}
	return hits
}

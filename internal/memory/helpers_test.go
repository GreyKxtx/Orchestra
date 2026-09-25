package memory

import "strings"

func (s *Store) FormatInject(maxBytes int) string {
	block, _, _ := s.FormatInjectReport(maxBytes)
	return block
}

func (s *Store) LazyOrchestra(dir string) string {
	content, _ := s.LazyOrchestraFile(dir)
	return content
}

// formatEntry renders one memory entry for the file. The type is always
// written out, including "project": the file is read by a person as well as
// by the model, and a marker that appears only sometimes is harder to scan
// than one that always does.
func formatEntry(timestamp, entryType, content string) string {
	return formatEntryFrom(timestamp, entryType, content, "")
}

// joinEntriesByPriority renders entries in injection order: pinned first,
// then by type, and within each type most recent first. Ordering itself lives
// in orderEntriesByPriority, which the budget-aware slicer needs as a slice.
//
// entries arrive oldest-first, as the file holds them.
func joinEntriesByPriority(entries []string) string {
	ordered := orderEntriesByPriority(entries)
	if len(ordered) == 0 {
		return ""
	}
	return strings.Join(ordered, entrySep+"\n")
}

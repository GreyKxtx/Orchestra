package memory

import (
	"fmt"
	"strings"
)

// Memory entries carry a type, so injection can spend the budget on what is
// most expensive to lose rather than on whatever happens to be newest.
//
// The field run that motivated this ended with one durable fact in fifty-two
// sessions, and agent.md is otherwise a flat chronological log: a correction
// the user made in week one sinks below a hundred "read file X" notes and is
// gone from the injected slice long before it stops being true.
const (
	// TypeFeedback is guidance the user gave about how to work — a
	// correction, a confirmed approach. The most expensive kind to lose,
	// because the user has to give it again.
	TypeFeedback = "feedback"
	// TypeUser is who the user is: role, preferences, working style.
	TypeUser = "user"
	// TypeProject is a fact about this codebase that the code does not say.
	// This is the default: every note written before types existed is one.
	TypeProject = "project"
	// TypeReference points at something external — a doc, a dashboard, a
	// ticket. Cheap to re-find, so it yields the budget first.
	TypeReference = "reference"
)

// injectionOrder is the priority used when slicing memory into a prompt.
var injectionOrder = []string{TypeFeedback, TypeUser, TypeProject, TypeReference}

// NormalizeEntryType maps user or model input onto a known type. Anything
// unrecognised — including empty — is a project fact, which is what an
// untyped note has always been.
func NormalizeEntryType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case TypeFeedback:
		return TypeFeedback
	case TypeUser:
		return TypeUser
	case TypeReference:
		return TypeReference
	default:
		return TypeProject
	}
}

// EntryTypeOf reads the type marker from a stored entry. Entries written
// before types existed have no marker and read as project facts.
func EntryTypeOf(entry string) string {
	// The marker lives on the timestamp line — "*<ts>* [feedback]" — so a
	// [pin] inside the content is never mistaken for a type.
	line, _, _ := strings.Cut(strings.TrimSpace(entry), "\n")
	open := strings.LastIndex(line, "[")
	if open < 0 || !strings.HasSuffix(strings.TrimSpace(line), "]") {
		return TypeProject
	}
	marker := strings.TrimSpace(line[open+1 : strings.LastIndex(line, "]")])
	switch strings.ToLower(marker) {
	case TypeFeedback, TypeUser, TypeProject, TypeReference:
		return NormalizeEntryType(marker)
	default:
		return TypeProject
	}
}

// formatEntry renders one memory entry for the file. The type is always
// written out, including "project": the file is read by a person as well as
// by the model, and a marker that appears only sometimes is harder to scan
// than one that always does.
func formatEntry(timestamp, entryType, content string) string {
	return fmt.Sprintf("%s*%s* [%s]\n\n%s\n", entrySep, timestamp, NormalizeEntryType(entryType), content)
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

// reverseEntries returns entries most-recent-first.
func reverseEntries(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, e := range in {
		out[len(in)-1-i] = e
	}
	return out
}

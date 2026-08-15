package lessons

import (
	"os"
	"strings"
)

// LastEntryOfKind returns the newest lesson entry of the given kind for a dept.
func LastEntryOfKind(projectRoot, dept string, kind Kind) (Entry, bool) {
	path := lessonPath(projectRoot, dept)
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false
	}
	parts := strings.Split(string(data), "\n## ")
	for i := len(parts) - 1; i >= 1; i-- {
		block := "## " + parts[i]
		e, ok := parseEntryBlock(block)
		if !ok {
			continue
		}
		if e.Kind != kind {
			continue
		}
		e.Dept = NormalizeDept(dept)
		return e, true
	}
	return Entry{}, false
}

func parseEntryBlock(block string) (Entry, bool) {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return Entry{}, false
	}
	header := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(header, "## ") {
		return Entry{}, false
	}
	parts := strings.Split(header, " · ")
	if len(parts) < 3 {
		return Entry{}, false
	}
	kind := Kind(strings.TrimSpace(parts[1]))
	var e Entry
	e.Kind = kind
	for _, ln := range lines[1:] {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "- task:"):
			e.Task = strings.TrimSpace(strings.TrimPrefix(ln, "- task:"))
		case strings.HasPrefix(ln, "- files:"):
			raw := strings.TrimSpace(strings.TrimPrefix(ln, "- files:"))
			for _, p := range strings.Split(raw, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					e.Files = append(e.Files, p)
				}
			}
		case strings.HasPrefix(ln, "- tools:"):
			e.Tools = strings.TrimSpace(strings.TrimPrefix(ln, "- tools:"))
		case strings.HasPrefix(ln, "- verify:"):
			e.Verify = strings.TrimSpace(strings.TrimPrefix(ln, "- verify:"))
		case strings.HasPrefix(ln, "- fix:"):
			e.Fix = strings.TrimSpace(strings.TrimPrefix(ln, "- fix:"))
		case strings.HasPrefix(ln, "- note:"):
			e.Note = strings.TrimSpace(strings.TrimPrefix(ln, "- note:"))
		}
	}
	return e, true
}

// FormatPatternForPromote turns a pattern lesson into overlay body text.
func FormatPatternForPromote(e Entry) string {
	var b strings.Builder
	if task := strings.TrimSpace(e.Task); task != "" {
		b.WriteString("- From task: ")
		b.WriteString(task)
		b.WriteByte('\n')
	}
	if len(e.Files) > 0 {
		b.WriteString("- Files: ")
		b.WriteString(strings.Join(e.Files, ", "))
		b.WriteByte('\n')
	}
	if tools := strings.TrimSpace(e.Tools); tools != "" {
		b.WriteString("- Tools: ")
		b.WriteString(tools)
		b.WriteByte('\n')
	}
	if verify := strings.TrimSpace(e.Verify); verify != "" {
		b.WriteString("- Verify: ")
		b.WriteString(verify)
		b.WriteByte('\n')
	}
	if fix := strings.TrimSpace(e.Fix); fix != "" {
		b.WriteString("- Fix: ")
		b.WriteString(fix)
		b.WriteByte('\n')
	}
	if note := strings.TrimSpace(e.Note); note != "" {
		b.WriteString("- ")
		b.WriteString(note)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

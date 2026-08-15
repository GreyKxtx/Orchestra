// Package lessons stores rule-based episodic learning artifacts at
// .orchestra/memory/lessons/<dept>.md — append-only, no LLM required.
// Runtime writes after worker verify; agents may append via memory_write
// (scoped). Injected into child worker prompts as <dept_lessons>.
package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

const (
	// RelDir is the lessons root relative to project root.
	RelDir = ".orchestra/memory/lessons"

	maxStoredEntries   = 48
	defaultInjectKeep  = 5
	defaultInjectBytes = 900
	// MaxAgentNoteBytes caps memory_write notes routed to dept lessons.
	MaxAgentNoteBytes = 400
)

var deptNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(@[a-z0-9][a-z0-9_-]*)?$`)

// Kind classifies an episodic lesson entry.
type Kind string

const (
	KindPattern      Kind = "pattern"
	KindAntiPattern  Kind = "anti_pattern"
	KindEscalation   Kind = "escalation"
	KindAgentNote    Kind = "agent_note"
)

// Entry is one append-only lesson record.
type Entry struct {
	Dept   string
	Kind   Kind
	Task   string
	Files  []string
	Tools  string
	Verify string
	Fix    string
	Note   string // agent_note body
}

// NormalizeDept returns a safe dept key or "engineering" when empty/invalid.
func NormalizeDept(dept string) string {
	dept = strings.ToLower(strings.TrimSpace(dept))
	if dept == "" {
		return "engineering"
	}
	if !deptNameRe.MatchString(dept) {
		return "engineering"
	}
	return dept
}

func lessonPath(projectRoot, dept string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(RelDir), NormalizeDept(dept)+".md")
}

// ClipAgentNote trims agent_note content for memory_write routing.
func ClipAgentNote(s string) string {
	return clipLine(s, MaxAgentNoteBytes)
}

// AppendAgentNote writes a dept-scoped agent note (deduped like other entries).
func AppendAgentNote(projectRoot, dept, note string) error {
	note = ClipAgentNote(note)
	if note == "" {
		return fmt.Errorf("content must not be empty")
	}
	return Append(projectRoot, Entry{
		Dept: dept,
		Kind: KindAgentNote,
		Note: note,
	})
}

// Append writes one entry (deduped against the last record with same kind+task+verify).
func Append(projectRoot string, e Entry) error {
	if projectRoot == "" {
		return nil
	}
	body := formatEntry(e)
	if body == "" {
		return nil
	}
	dept := NormalizeDept(e.Dept)
	path := lessonPath(projectRoot, dept)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("lessons mkdir: %w", err)
	}
	if dup, err := isDuplicate(path, body); err != nil {
		return err
	} else if dup {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("lessons open: %w", err)
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("lessons write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	return trimFile(path, maxStoredEntries)
}

func formatEntry(e Entry) string {
	kind := strings.TrimSpace(string(e.Kind))
	if kind == "" {
		kind = string(KindPattern)
	}
	ts := time.Now().UTC().Format("2006-01-02 15:04")
	dept := NormalizeDept(e.Dept)
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s · %s · %s\n", ts, kind, dept)
	if task := strings.TrimSpace(e.Task); task != "" {
		fmt.Fprintf(&b, "- task: %s\n", clipLine(task, 200))
	}
	if len(e.Files) > 0 {
		fmt.Fprintf(&b, "- files: %s\n", clipLine(strings.Join(e.Files, ", "), 240))
	}
	if tools := strings.TrimSpace(e.Tools); tools != "" {
		fmt.Fprintf(&b, "- tools: %s\n", clipLine(tools, 160))
	}
	if verify := strings.TrimSpace(e.Verify); verify != "" {
		fmt.Fprintf(&b, "- verify: %s\n", clipLine(verify, 240))
	}
	if fix := strings.TrimSpace(e.Fix); fix != "" {
		fmt.Fprintf(&b, "- fix: %s\n", clipLine(fix, 200))
	}
	if note := strings.TrimSpace(e.Note); note != "" {
		fmt.Fprintf(&b, "- note: %s\n", clipLine(note, 400))
	}
	if b.Len() <= len("\n## ") {
		return ""
	}
	return b.String()
}

func isDuplicate(path, body string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return true, nil
	}
	// Compare semantic core (skip timestamp line).
	core := entryCore(trimmed)
	prev := lastEntryCore(string(data))
	return core != "" && core == prev, nil
}

func entryCore(entry string) string {
	lines := strings.Split(entry, "\n")
	var b strings.Builder
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "## ") {
			continue
		}
		if ln == "" {
			continue
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func lastEntryCore(fileBody string) string {
	parts := strings.Split(fileBody, "\n## ")
	if len(parts) <= 1 {
		return entryCore(fileBody)
	}
	return entryCore("## " + parts[len(parts)-1])
}

func trimFile(path string, keep int) error {
	if keep <= 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parts := strings.Split(string(data), "\n## ")
	if len(parts) <= keep+1 {
		return nil
	}
	header := parts[0]
	entries := parts[1:]
	entries = entries[len(entries)-keep:]
	var b strings.Builder
	b.WriteString(strings.TrimRight(header, "\n"))
	for _, e := range entries {
		b.WriteString("\n## ")
		b.WriteString(e)
	}
	out := strings.TrimRight(b.String(), "\n") + "\n"
	return fsutil.AtomicWriteFile(path, []byte(out), 0o644)
}

// Tail returns the last maxBytes of the dept lesson file.
func Tail(projectRoot, dept string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultInjectBytes
	}
	path := lessonPath(projectRoot, dept)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return ""
	}
	if len(body) <= maxBytes {
		return body
	}
	cut := body[len(body)-maxBytes:]
	if idx := strings.Index(cut, "\n## "); idx >= 0 {
		cut = cut[idx+1:]
	}
	rel := filepath.ToSlash(filepath.Join(RelDir, NormalizeDept(dept)+".md"))
	return "…(older lessons truncated; read " + rel + ")\n" + cut
}

// FormatInject wraps Tail for worker/lead prompt injection.
func FormatInject(projectRoot, dept string) string {
	dept = NormalizeDept(dept)
	tail := Tail(projectRoot, dept, defaultInjectBytes)
	if tail == "" {
		return ""
	}
	rel := filepath.ToSlash(filepath.Join(RelDir, dept+".md"))
	return "<dept_lessons source=\"" + rel + "\">\n" + tail + "\n</dept_lessons>"
}

// IsDeptScope reports whether a memory_write scope maps to a lessons file.
func IsDeptScope(scope string) bool {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" || scope == "project" || scope == "session" || scope == "global" {
		return false
	}
	return deptNameRe.MatchString(scope)
}

func clipLine(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

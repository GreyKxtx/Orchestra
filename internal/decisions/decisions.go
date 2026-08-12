// Package decisions owns .orchestra/decisions.md — the append-only decision
// log of the Question Barrier (spec §4.3, ADR-2). The Go runtime writes
// question/answer pairs, waivers and assumption records verbatim; no LLM
// rephrases them. Leads receive the tail of the log injected into their
// prompts so cross-department answers survive without shared chat history.
package decisions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileRel is the decision log path relative to the project root.
const FileRel = ".orchestra/decisions.md"

// Entry is one appended record.
type Entry struct {
	Kind     string // "qa" | "assumption" | "waiver" | "decision"
	Dept     string // originating department/instance, optional
	Question string
	Answer   string
}

// Append writes entries to the log (creates the file with a header on first
// use). Append-only by contract: existing content is never rewritten.
func Append(projectRoot string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(FileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", FileRel, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	var b strings.Builder
	if st.Size() == 0 {
		b.WriteString("# Decision log\n\nAppend-only. Written by the Orchestra runtime (Question Barrier); do not edit past entries.\n")
	}
	ts := time.Now().UTC().Format("2006-01-02 15:04")
	for _, e := range entries {
		kind := strings.TrimSpace(e.Kind)
		if kind == "" {
			kind = "decision"
		}
		fmt.Fprintf(&b, "\n## %s · %s", ts, kind)
		if d := strings.TrimSpace(e.Dept); d != "" {
			fmt.Fprintf(&b, " · %s", d)
		}
		b.WriteString("\n")
		if q := strings.TrimSpace(e.Question); q != "" {
			fmt.Fprintf(&b, "- Q: %s\n", q)
		}
		if a := strings.TrimSpace(e.Answer); a != "" {
			fmt.Fprintf(&b, "- A: %s\n", a)
		}
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("append %s: %w", FileRel, err)
	}
	return f.Sync()
}

// Adopted reports whether the project runs in orchestra mode (state file
// exists) — the decision log is only maintained for orchestrated sessions.
func Adopted(projectRoot string) bool {
	_, err := os.Stat(filepath.Join(projectRoot, ".orchestra", "state.md"))
	return err == nil
}

// Tail returns the last maxBytes of the log for prompt injection (whole file
// when it fits, "" when the log does not exist). Cuts on an entry boundary
// where possible so the injected block starts with a complete record.
func Tail(projectRoot string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(FileRel)))
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
	return "…(older entries truncated; read " + FileRel + " for the full log)\n" + cut
}

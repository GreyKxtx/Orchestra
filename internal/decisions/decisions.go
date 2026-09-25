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

	"github.com/orchestra/orchestra/patch/fsutil"
)

// FileRel is the decision log path relative to the project root.
const FileRel = ".orchestra/decisions.md"

// ArchiveFileRel is where the log's older entries go once it passes
// MaxBytes: Leads read the tail, and the file was read whole on every
// prompt (DATA-11). The archive is append-only like the log.
const ArchiveFileRel = ".orchestra/decisions.archive.md"

// MaxBytes is the size past which Append moves the older half of the
// entries to ArchiveFileRel.
const MaxBytes = 512 << 10

// maxBytes is MaxBytes, overridable by tests.
var maxBytes = MaxBytes

// archiveNote is the line the log carries once entries were archived.
const archiveNote = "> Older entries are in " + ArchiveFileRel + "\n"

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
	// Two cores can serve one project; the lock keeps their appends whole
	// and the archiving below from racing an append.
	unlock, err := fsutil.LockFile(path + ".lock")
	if err != nil {
		return fmt.Errorf("lock %s: %w", FileRel, err)
	}
	defer unlock()
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
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return archiveOverflow(path)
}

// archiveOverflow moves the older entries of the log at path to the archive
// beside it once the log is past maxBytes, keeping the newest half. Entries
// are moved whole (they start at "\n## "), the header stays, and a note
// under it says where the rest went. Caller holds the log's lock.
func archiveOverflow(path string) error {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= int64(maxBytes) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	body := string(data)
	first := strings.Index(body, "\n## ")
	if first < 0 {
		return nil
	}
	header := strings.Replace(body[:first+1], archiveNote, "", 1)
	entries := body[first+1:]
	keep := maxBytes / 2
	if len(entries) <= keep {
		return nil
	}
	cut := len(entries) - keep
	idx := strings.Index(entries[cut:], "\n## ")
	if idx < 0 {
		return nil
	}
	cut += idx + 1
	older, newer := entries[:cut], entries[cut:]

	archive := filepath.Join(filepath.Dir(path), filepath.Base(filepath.FromSlash(ArchiveFileRel)))
	af, err := os.OpenFile(archive, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", ArchiveFileRel, err)
	}
	if ast, err := af.Stat(); err == nil && ast.Size() == 0 {
		_, _ = af.WriteString("# Decision log archive\n\nOlder entries of " + FileRel + ", moved here by the Orchestra runtime; append-only.\n")
	}
	if _, err := af.WriteString("\n" + strings.TrimRight(older, "\n") + "\n"); err != nil {
		_ = af.Close()
		return fmt.Errorf("append %s: %w", ArchiveFileRel, err)
	}
	if err := af.Sync(); err != nil {
		_ = af.Close()
		return err
	}
	if err := af.Close(); err != nil {
		return err
	}
	// Only once the archive holds them: a crash between the two writes
	// duplicates entries, never loses them.
	return fsutil.AtomicWriteFile(path, []byte(header+archiveNote+"\n"+newer), 0o644)
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
	return fmt.Sprintf("…(%d bytes of older entries not shown; read %s for the full log)\n", len(body)-len(cut), FileRel) + cut
}

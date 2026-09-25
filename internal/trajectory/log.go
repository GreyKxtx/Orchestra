package trajectory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// Path returns the sidecar's location for a session.
//
// The .jsonl extension is not cosmetic: sessionfile.ListMeta treats every
// .json file in this directory as a session, so a sidecar named
// "<id>.events.json" would appear in the session list as a phantom session
// called "<id>.events".
func Path(workspaceRoot, sessionID string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions", sessionID+".events.jsonl")
}

// MaxEventBytes caps one event's payload. A larger payload is recorded as
// {"truncated": true, "bytes": N} in its place, so the line stays readable
// and the log stays a log (DATA-9: one line past the reader's limit used to
// disable recording for the session for good).
const MaxEventBytes = 1 << 20

// MaxLogBytes is where a session's log rotates: the current file becomes
// <name>.1, replacing the previous generation, and a new one starts. Read
// returns both generations, so a session keeps up to twice this much.
const MaxLogBytes = 32 << 20

// maxLogBytes is MaxLogBytes, overridable by tests.
var maxLogBytes int64 = MaxLogBytes

// Writer appends events to one session's log.
type Writer struct {
	mu   sync.Mutex
	f    *os.File
	seq  int64
	size int64
	path string
	// tornWrite is set when a write failed and may have left a partial line.
	// The next append starts on a fresh line so it cannot merge into it.
	tornWrite bool
}

// NewWriter opens a session's log for appending, continuing its sequence.
//
// One writer per session at a time. Two concurrent writers would each read the
// same on-disk maximum and hand out the same next seq; nothing here prevents
// that, because the core already serialises turns within a session. A caller
// that ever runs two turns against one session must serialise them itself.
func NewWriter(workspaceRoot, sessionID string) (*Writer, error) {
	if workspaceRoot == "" || sessionID == "" {
		return nil, fmt.Errorf("trajectory: workspace_root and session_id required")
	}
	if err := sessionfile.CheckID(sessionID); err != nil {
		return nil, err
	}
	return openWriter(Path(workspaceRoot, sessionID))
}

// openWriter opens the log at p for appending, repairing a torn tail and
// continuing its sequence.
func openWriter(p string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("trajectory: mkdir: %w", err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	// Repair a torn tail before anything else. A crash can stop a write
	// anywhere, including between a line's last byte and its newline, so the
	// file may end mid-line. Appending onto that merges the fragment and the
	// next event into one unparseable line — losing both — and lastSeq would
	// then reissue the fragment's number to different content.
	//
	// Terminating the fragment rather than truncating it is deliberate: a
	// write interrupted just before its newline leaves a COMPLETE, parseable
	// event, and truncation would throw that away. One newline destroys
	// nothing, and an append-only log has no business rewriting its history.
	if err := terminateTornTail(f, p); err != nil {
		_ = f.Close()
		return nil, err
	}
	// After the repair, so a recovered final line counts toward the sequence.
	// The sequence continues across turns, so it starts from what is already
	// on disk. Restarting at 1 would give two events the same seq and the log
	// would stop being ordered.
	last, err := lastSeq(p)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	// A log that just rotated is empty; its sequence went on in the older
	// generation, and restarting it would number two events the same.
	if older, err := lastSeq(p + ".1"); err == nil && older > last {
		last = older
	}
	var size int64
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	return &Writer{f: f, seq: last, size: size, path: p}, nil
}

// rotate moves the current log to <path>.1, replacing the previous
// generation, and starts a new one. Best-effort: if the rename fails the log
// keeps growing in place, which loses nothing.
func (w *Writer) rotate() {
	_ = w.f.Close()
	older := w.path + ".1"
	_ = os.Remove(older)
	if err := os.Rename(w.path, older); err == nil {
		w.size = 0
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		// Appends fail until the next NewWriter; an *os.File that is nil
		// answers ErrInvalid rather than panicking.
		w.f = nil
		return
	}
	w.f = f
}

// truncatedPayload stands in for a payload past MaxEventBytes.
func truncatedPayload(n int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"truncated":true,"bytes":%d}`, n))
}

// terminateTornTail ends the file with a newline if it does not already, so
// the next append starts on a line of its own.
func terminateTornTail(f *os.File, path string) error {
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("trajectory: stat %s: %w", path, err)
	}
	if st.Size() == 0 {
		return nil
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("trajectory: reopen %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()
	var b [1]byte
	if _, err := r.ReadAt(b[:], st.Size()-1); err != nil {
		return fmt.Errorf("trajectory: read tail of %s: %w", path, err)
	}
	if b[0] == '\n' {
		return nil
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("trajectory: terminate torn tail of %s: %w", path, err)
	}
	return nil
}

// Append writes one event. data is stored as sent; nil is stored as no payload.
func (w *Writer) Append(eventType string, data any) error {
	if w == nil {
		return nil
	}
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("trajectory: marshal payload: %w", err)
		}
		raw = b
		if len(raw) > MaxEventBytes {
			raw = truncatedPayload(len(raw))
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	line, err := json.Marshal(Event{
		Seq:    w.seq,
		TimeMS: time.Now().UTC().UnixMilli(),
		Type:   eventType,
		Source: SourceCore,
		Data:   raw,
	})
	if err != nil {
		// Safe to roll back: nothing has reached the file yet.
		w.seq--
		return fmt.Errorf("trajectory: marshal event: %w", err)
	}
	// One write for line+newline: a single short write is what makes a torn
	// tail a torn *line*, which NewWriter terminates and Read skips.
	buf := append(line, '\n')
	if w.tornWrite {
		// A previous write failed and may have left a partial line. Start on a
		// line of our own rather than merging into it.
		buf = append([]byte{'\n'}, buf...)
	}
	if w.size > 0 && w.size+int64(len(buf)) > maxLogBytes {
		w.rotate()
	}
	if _, err := w.f.Write(buf); err != nil {
		// Deliberately no `w.seq--` here, unlike the marshal failure above: a
		// failed write may have left a partial line, so reusing this number
		// could give two different events the same seq. A gap is visible and
		// harmless; a duplicate is silent corruption.
		w.tornWrite = true
		return fmt.Errorf("trajectory: append to %s: %w", w.path, err)
	}
	w.tornWrite = false
	w.size += int64(len(buf))
	return nil
}

// Close releases the file. Appends after Close fail.
func (w *Writer) Close() error {
	if w == nil || w.f == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.f.Close()
	w.f = nil
	if err != nil {
		return fmt.Errorf("trajectory: close %s: %w", w.path, err)
	}
	return nil
}

// Read returns a session's events. recorded is false when no log exists for
// the session at all, which is a different answer from a log with no events:
// one means "this session predates the log", the other "nothing happened yet".
func Read(workspaceRoot, sessionID string) (events []Event, recorded bool, err error) {
	if err := sessionfile.CheckID(sessionID); err != nil {
		return nil, false, err
	}
	return readGenerations(Path(workspaceRoot, sessionID))
}

// readGenerations returns the rotated generation's events, if any, followed
// by the current file's.
func readGenerations(p string) (events []Event, recorded bool, err error) {
	older, recOlder, err := readFile(p + ".1")
	if err != nil {
		return nil, recOlder, err
	}
	current, recCurrent, err := readFile(p)
	if err != nil {
		return nil, recOlder || recCurrent, err
	}
	if !recOlder && !recCurrent {
		return nil, false, nil
	}
	out := append([]Event{}, older...)
	return append(out, current...), true, nil
}

// readFile reads the log at p; recorded is false when it does not exist.
func readFile(p string) (events []Event, recorded bool, err error) {
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	defer func() { _ = f.Close() }()

	out := []Event{}
	err = eachLine(f, func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Any line that will not parse is skipped, wherever it sits — not
			// only a torn tail. That is deliberate: one unreadable line must
			// not deny a reader the rest of the log, and a log outlives the
			// code that reads it. The cost is that a corrupt line in the middle
			// of a file is as quiet as an expected one at the end.
			return
		}
		out = append(out, ev)
	})
	if err != nil {
		return nil, true, fmt.Errorf("trajectory: read %s: %w", p, err)
	}
	return out, true, nil
}

// eachLine calls fn with every non-empty line of r, whatever its length. A
// bufio.Scanner stops for good at a line past its buffer, and one such line
// — a tool result of several megabytes — used to end recording for the
// session, since every NewWriter re-read the log (DATA-9).
func eachLine(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if line = bytes.TrimRight(line, "\r\n"); len(line) > 0 {
			fn(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func lastSeq(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("trajectory: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var last int64
	err = eachLine(f, func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return
		}
		if ev.Seq > last {
			last = ev.Seq
		}
	})
	if err != nil {
		return 0, fmt.Errorf("trajectory: scan %s: %w", path, err)
	}
	return last, nil
}

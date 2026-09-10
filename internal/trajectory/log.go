package trajectory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// Writer appends events to one session's log.
type Writer struct {
	mu   sync.Mutex
	f    *os.File
	seq  int64
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
	p := Path(workspaceRoot, sessionID)
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
	return &Writer{f: f, seq: last, path: p}, nil
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
		w.seq--
		return fmt.Errorf("trajectory: marshal event: %w", err)
	}
	// One write for line+newline: a single short write is what makes a torn
	// tail a torn *line*, which Read discards cleanly.
	buf := append(line, '\n')
	if w.tornWrite {
		// A previous write failed and may have left a partial line. Start on a
		// line of our own rather than merging into it.
		buf = append([]byte{'\n'}, buf...)
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
	p := Path(workspaceRoot, sessionID)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	defer func() { _ = f.Close() }()

	out := []Event{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Any line that will not parse is skipped, wherever it sits — not
			// only a torn tail. That is deliberate: one unreadable line must
			// not deny a reader the rest of the log, and a log outlives the
			// code that reads it. The cost is that a corrupt line in the middle
			// of a file is as quiet as an expected one at the end.
			continue
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, true, fmt.Errorf("trajectory: read %s: %w", p, err)
	}
	return out, true, nil
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
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Seq > last {
			last = ev.Seq
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("trajectory: scan %s: %w", path, err)
	}
	return last, nil
}

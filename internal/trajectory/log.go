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
}

// NewWriter opens a session's log for appending, continuing its sequence.
func NewWriter(workspaceRoot, sessionID string) (*Writer, error) {
	if workspaceRoot == "" || sessionID == "" {
		return nil, fmt.Errorf("trajectory: workspace_root and session_id required")
	}
	p := Path(workspaceRoot, sessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("trajectory: mkdir: %w", err)
	}
	// The sequence continues across turns, so it starts from what is already
	// on disk. Restarting at 1 would give two events the same seq and the log
	// would stop being ordered.
	last, err := lastSeq(p)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	return &Writer{f: f, seq: last, path: p}, nil
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
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("trajectory: append to %s: %w", w.path, err)
	}
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
			// A torn trailing line from an interrupted write. Everything
			// complete before it is still good, which is the whole reason the
			// log is line-oriented.
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

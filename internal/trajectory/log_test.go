package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestAppendAndRead_RoundTripsWithContiguousSeq(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, recorded, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("recorded = false, want true — the sidecar exists")
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Errorf("events[%d].Seq = %d, want %d — sequence must be contiguous from 1", i, ev.Seq, i+1)
		}
		if ev.Type != "agent/event" {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, "agent/event")
		}
		if ev.Source != "core" {
			t.Errorf("events[%d].Source = %q, want %q", i, ev.Source, "core")
		}
		if ev.TimeMS <= 0 {
			t.Errorf("events[%d].TimeMS = %d, want a real timestamp", i, ev.TimeMS)
		}
	}
}

func TestRead_MissingSidecarIsNotRecordedRatherThanEmpty(t *testing.T) {
	root := t.TempDir()
	events, recorded, err := Read(root, "never-ran")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if recorded {
		t.Error("recorded = true, want false — there is no sidecar for this session")
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d, want 0", len(events))
	}
}

func TestRead_TornTrailingLineKeepsEveryCompleteEventBeforeIt(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a crash mid-write: a partial third line with no newline.
	f, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for torn write: %v", err)
	}
	if _, err := f.WriteString(`{"seq":3,"type":"agent/ev`); err != nil {
		t.Fatalf("torn write: %v", err)
	}
	_ = f.Close()

	events, recorded, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read must not fail on a torn trailing line: %v", err)
	}
	if !recorded {
		t.Error("recorded = false, want true")
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 — the torn line is dropped, the complete ones survive", len(events))
	}
}

func TestAppend_ContinuesTheSequenceAcrossWriters(t *testing.T) {
	root := t.TempDir()
	w1, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w1.Append("a", nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A second turn in the same session opens a new writer. The sequence must
	// continue rather than restart, or two events share a seq and the log
	// stops being ordered.
	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter again: %v", err)
	}
	if err := w2.Append("b", nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("seqs = %d,%d — want 1,2", events[0].Seq, events[1].Seq)
	}
}

func TestPath_UsesJSONLSoItIsNotMistakenForASession(t *testing.T) {
	// sessionfile.ListMeta treats every .json file in the sessions directory
	// as a session. A sidecar ending in .json would show up in the session
	// list as a phantom session named "<id>.events".
	got := filepath.Base(Path(t.TempDir(), "s1"))
	if got != "s1.events.jsonl" {
		t.Errorf("Path base = %q, want %q", got, "s1.events.jsonl")
	}
}

func TestAppend_DataIsStoredAsGivenNotStringified(t *testing.T) {
	root := t.TempDir()
	w, _ := NewWriter(root, "s1")
	if err := w.Append("agent/event", map[string]any{"type": "tool_call_start", "step": 2}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = w.Close()

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var payload struct {
		Type string `json:"type"`
		Step int    `json:"step"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatalf("Data is not a JSON object: %v (raw: %s)", err, events[0].Data)
	}
	if payload.Type != "tool_call_start" || payload.Step != 2 {
		t.Errorf("payload = %+v, want {tool_call_start 2}", payload)
	}
}

func TestPath_MatchesTheNameSessionfileDeletes(t *testing.T) {
	// internal/sessionfile.Delete builds this same name by hand rather than
	// importing this package, to keep the dependency pointing one way. If the
	// two ever disagree, deleting a session silently orphans its log.
	root := t.TempDir()
	want := filepath.Join(root, ".orchestra", "sessions", "s1.events.jsonl")
	if got := Path(root, "s1"); got != want {
		t.Errorf("Path = %q, want %q — and internal/sessionfile.Delete must be updated with it", got, want)
	}
}

func TestAppend_AfterCloseFails(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Append("agent/event", nil); err == nil {
		t.Error("Append after Close returned nil, want an error — the doc comment promises it fails")
	}
}

func TestNewWriter_AfterATornTailKeepsWritingWithoutCorruption(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Crash mid-write: a partial line with no newline.
	f, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for torn write: %v", err)
	}
	if _, err := f.WriteString(`{"seq":3,"type":"agent/ev`); err != nil {
		t.Fatalf("torn write: %v", err)
	}
	_ = f.Close()

	// Restart and keep recording.
	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter after crash: %v", err)
	}
	if err := w2.Append("agent/event", map[string]any{"n": 4}); err != nil {
		t.Fatalf("Append after crash: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Both pre-crash events plus the new one. Before the fix this returned 2:
	// the new event merged into the fragment and both were dropped.
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3 — the event written after the crash was swallowed", len(events))
	}
	seen := map[int64]bool{}
	for _, ev := range events {
		if seen[ev.Seq] {
			t.Errorf("seq %d appears twice — a number was reissued to different content", ev.Seq)
		}
		seen[ev.Seq] = true
	}
	if events[len(events)-1].Seq <= events[len(events)-2].Seq {
		t.Errorf("sequence did not advance: %v then %v", events[len(events)-2].Seq, events[len(events)-1].Seq)
	}
}

func TestNewWriter_ATailInterruptedBeforeItsNewlineIsRecovered(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Append("agent/event", map[string]any{"n": 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A write stopped between the last byte of a complete event and its
	// newline. The event is intact; only its terminator is missing. This is
	// why the fix terminates the tail instead of truncating it — truncation
	// would destroy a good event.
	f, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	line := `{"seq":2,"time_ms":1,"type":"agent/event","source":"core","data":{"n":2}}`
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("unterminated write: %v", err)
	}
	_ = f.Close()

	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w2.Append("agent/event", map[string]any{"n": 3}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3 — the unterminated but complete event must survive", len(events))
	}
	if events[2].Seq != 3 {
		t.Errorf("events[2].Seq = %d, want 3 — the sequence must resume past the recovered line", events[2].Seq)
	}
}

func TestAppend_AFailedWriteDoesNotReissueItsSequenceNumber(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Append("agent/event", map[string]any{"n": 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Force a write failure by swapping in a closed descriptor. This test is
	// inside the package, so it can reach w.f — that is the only way to
	// exercise a write error without an injectable writer.
	good := w.f
	broken, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open second handle: %v", err)
	}
	_ = broken.Close()
	w.f = broken
	if err := w.Append("agent/event", map[string]any{"n": 2}); err == nil {
		t.Fatal("Append on a closed descriptor returned nil, want an error")
	}
	w.f = good

	if err := w.Append("agent/event", map[string]any{"n": 3}); err != nil {
		t.Fatalf("Append after the failure: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 — the failed write recorded nothing", len(events))
	}
	// A gap is expected and acceptable; a reused number is not.
	if events[0].Seq == events[1].Seq {
		t.Errorf("both events carry seq %d — the failed write's number was reissued", events[0].Seq)
	}
	if events[1].Seq <= events[0].Seq {
		t.Errorf("sequence went backwards: %d then %d", events[0].Seq, events[1].Seq)
	}
}

func setMaxLogBytes(t *testing.T, n int64) {
	t.Helper()
	old := maxLogBytes
	maxLogBytes = n
	t.Cleanup(func() { maxLogBytes = old })
}

// Past the cap the log rotates: the current file becomes <name>.1 and a new
// one starts. Read returns both generations, in order, with the sequence
// unbroken; a session's log no longer grows without bound (DATA-9).
func TestAppend_RotatesPastTheCapAndReadReturnsEveryGeneration(t *testing.T) {
	setMaxLogBytes(t, 1200) // ~100 bytes an event: one rotation in 20 events
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i, "pad": "0123456789abcdef"}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(root, "s1") + ".1"); err != nil {
		t.Fatalf("no rotated generation: %v", err)
	}
	if st, _ := os.Stat(Path(root, "s1")); st.Size() > 1200 {
		t.Fatalf("current log is %d bytes, past the cap", st.Size())
	}
	events, recorded, err := Read(root, "s1")
	if err != nil || !recorded {
		t.Fatalf("Read: %v, recorded=%v", err, recorded)
	}
	// Two generations hold every event here; the sequence runs unbroken
	// across the rotation and ends at the last one written.
	if len(events) != 20 {
		t.Fatalf("len(events) = %d, want 20 across both generations", len(events))
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
}

// A writer opened on a log that just rotated continues the sequence from the
// older generation rather than restarting at 1.
func TestNewWriter_ContinuesTheSequenceFromTheRotatedGeneration(t *testing.T) {
	setMaxLogBytes(t, 150) // every append past the first rotates
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := w2.Append("agent/event", nil); err != nil {
		t.Fatal(err)
	}
	_ = w2.Close()
	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	var last int64
	for _, ev := range events {
		if seen[ev.Seq] {
			t.Fatalf("seq %d issued twice", ev.Seq)
		}
		seen[ev.Seq] = true
		if ev.Seq > last {
			last = ev.Seq
		}
	}
	if last != 5 {
		t.Fatalf("last seq = %d, want 5", last)
	}
}

// A payload past MaxEventBytes is recorded as a marker, not dropped and not
// written whole: the log stays readable line by line.
func TestAppend_AnOversizedPayloadIsRecordedAsTruncated(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	big := make([]byte, MaxEventBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := w.Append("agent/event", map[string]any{"content": string(big)}); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	events, _, err := Read(root, "s1")
	if err != nil || len(events) != 1 {
		t.Fatalf("Read: %v, %d events", err, len(events))
	}
	var marker struct {
		Truncated bool `json:"truncated"`
		Bytes     int  `json:"bytes"`
	}
	if err := json.Unmarshal(events[0].Data, &marker); err != nil || !marker.Truncated || marker.Bytes <= MaxEventBytes {
		t.Fatalf("data = %.80s… (%v), want a truncation marker", events[0].Data, err)
	}
}

// One line longer than any buffer must not end recording for the session:
// NewWriter re-reads the log for its sequence, and a bufio.Scanner stopped
// there for good, so every later turn of that session went unrecorded.
func TestNewWriter_AnOverlongLineDoesNotStopTheLog(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append("agent/event", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	f, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	junk := make([]byte, 9<<20)
	for i := range junk {
		junk[i] = 'j'
	}
	if _, err := f.Write(append(junk, '\n')); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter after a 9 MB line: %v", err)
	}
	if err := w2.Append("agent/event", map[string]any{"n": 2}); err != nil {
		t.Fatal(err)
	}
	_ = w2.Close()
	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read after a 9 MB line: %v", err)
	}
	if len(events) != 2 || events[1].Seq != 2 {
		t.Fatalf("events = %+v, want the two real ones with seq 1, 2", events)
	}
}

// Deleting a session takes the rotated generation with it.
func TestRotatedGeneration_IsDeletedWithTheSession(t *testing.T) {
	setMaxLogBytes(t, 150)
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_ = w.Append("agent/event", map[string]any{"n": i})
	}
	_ = w.Close()
	if _, err := os.Stat(Path(root, "s1") + ".1"); err != nil {
		t.Fatalf("no rotated generation: %v", err)
	}
	if err := sessionfile.Delete(root, "s1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{Path(root, "s1"), Path(root, "s1") + ".1"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the session's deletion", p)
		}
	}
}

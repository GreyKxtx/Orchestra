package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

package state

import (
	"testing"
	"time"
)

func TestSubagentTracker_QueuedStartedDone(t *testing.T) {
	tr := NewSubagentTracker()
	tr.OnQueued("b", "worker", `{"target_files":["handler.go"]}`, "waiting target_file lock")
	tr.OnStarted("a", "worker", `{"intent":"edit","target_files":["router.go","handler.go"]}`, time.Now())
	snaps := tr.Snapshot(time.Now())
	if len(snaps) != 2 {
		t.Fatalf("len=%d", len(snaps))
	}
	if snaps[0].TaskID != "b" || snaps[0].Status != "queued" {
		t.Fatalf("first=%+v", snaps[0])
	}
	if snaps[1].Goal != "router.go" || snaps[1].Status != "running" {
		t.Fatalf("second=%+v", snaps[1])
	}

	tr.OnDone("a", "worker", "done", "ok", "", time.Now().Add(time.Second))
	snaps = tr.Snapshot(time.Now())
	if snaps[1].Status != "done" || snaps[1].ResultSummary != "ok" {
		t.Fatalf("done=%+v", snaps[1])
	}
	if !tr.HasActive() {
		t.Fatal("queued B must keep HasActive")
	}
	tr.Reset()
	if tr.HasActive() || len(tr.Snapshot(time.Now())) != 0 {
		t.Fatal("reset must clear tracker")
	}
}

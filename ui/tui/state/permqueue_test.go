package state

import "testing"

func TestPermQueue_PushBecomesCurrent(t *testing.T) {
	var q PermQueue
	if !q.Push(PermRequest{ReqID: 1, Tool: "bash"}) {
		t.Fatal("first push must become current")
	}
	if cur, ok := q.Current(); !ok || cur.ReqID != 1 {
		t.Fatalf("current=%+v ok=%v", cur, ok)
	}
}

func TestPermQueue_SecondPushWaitsFIFO(t *testing.T) {
	var q PermQueue
	q.Push(PermRequest{ReqID: 1})
	if q.Push(PermRequest{ReqID: 2}) {
		t.Fatal("second push must queue, not present")
	}
	if q.Push(PermRequest{ReqID: 3}) {
		t.Fatal("third push must queue, not present")
	}
	if q.Waiting() != 2 {
		t.Fatalf("waiting=%d, want 2", q.Waiting())
	}

	// Answer #1, promote #2, answer it, promote #3 — strict FIFO.
	if r, ok := q.Answer(); !ok || r.ReqID != 1 {
		t.Fatalf("answer=%+v ok=%v", r, ok)
	}
	if r, ok := q.Promote(); !ok || r.ReqID != 2 {
		t.Fatalf("promote=%+v ok=%v, want ReqID 2", r, ok)
	}
	q.Answer()
	if r, ok := q.Promote(); !ok || r.ReqID != 3 {
		t.Fatalf("promote=%+v ok=%v, want ReqID 3", r, ok)
	}
}

func TestPermQueue_PromoteRefusesWhileCurrent(t *testing.T) {
	var q PermQueue
	q.Push(PermRequest{ReqID: 1})
	q.Push(PermRequest{ReqID: 2})
	if _, ok := q.Promote(); ok {
		t.Fatal("promote must refuse while a request is presented")
	}
}

func TestPermQueue_AnswerOnEmpty(t *testing.T) {
	var q PermQueue
	if _, ok := q.Answer(); ok {
		t.Fatal("answer on empty queue must report ok=false")
	}
}

func TestPermQueue_ResetDropsEverything(t *testing.T) {
	var q PermQueue
	q.Push(PermRequest{ReqID: 1})
	q.Push(PermRequest{ReqID: 2})
	q.Reset()
	if _, ok := q.Current(); ok {
		t.Fatal("reset must clear current")
	}
	if q.Waiting() != 0 {
		t.Fatalf("waiting=%d after reset", q.Waiting())
	}
	if _, ok := q.Promote(); ok {
		t.Fatal("nothing must be promotable after reset")
	}
}

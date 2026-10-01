package session

import "testing"

func TestInterjections_RefusedWithoutATurn(t *testing.T) {
	s := New()
	if _, ok := s.Interject("hello"); ok {
		t.Fatal("a message with no turn running must be refused, so the client sends it as a turn")
	}
}

func TestInterjections_DrainedOnceInOrder(t *testing.T) {
	s := New()
	buf := s.BeginInterjections()
	id1, ok1 := s.Interject("first")
	id2, ok2 := s.Interject("second")
	if !ok1 || !ok2 || id1 == "" || id1 == id2 {
		t.Fatalf("accept: ok=%v,%v ids=%q,%q", ok1, ok2, id1, id2)
	}
	got := buf.Drain(false)
	if len(got) != 2 || got[0].Content != "first" || got[1].Content != "second" || got[0].ID != id1 {
		t.Fatalf("drain = %+v", got)
	}
	if again := buf.Drain(false); len(again) != 0 {
		t.Fatalf("drained twice: %+v", again)
	}
}

// At the final step the agent either takes what is pending or closes the
// buffer in the same breath: a message can never slip in after the last look
// and be lost when the turn ends.
func TestInterjections_FinalDrainClosesWhenEmpty(t *testing.T) {
	s := New()
	buf := s.BeginInterjections()
	if got := buf.Drain(true); len(got) != 0 {
		t.Fatalf("nothing was sent, drain = %+v", got)
	}
	if _, ok := s.Interject("late"); ok {
		t.Fatal("after the final look the buffer must refuse")
	}
	// A final that failed keeps the loop going; the next step reopens.
	buf.Drain(false)
	if _, ok := s.Interject("after reopen"); !ok {
		t.Fatal("the next step must accept again")
	}
}

func TestInterjections_FinalDrainTakesPending(t *testing.T) {
	s := New()
	buf := s.BeginInterjections()
	s.Interject("wait, also do X")
	if got := buf.Drain(true); len(got) != 1 {
		t.Fatalf("pending message not taken at the final: %+v", got)
	}
	if _, ok := s.Interject("more"); !ok {
		t.Fatal("taking a message must leave the buffer open: the loop goes on")
	}
}

func TestInterjections_EndReturnsUndelivered(t *testing.T) {
	s := New()
	s.BeginInterjections()
	s.Interject("never seen")
	left := s.EndInterjections()
	if len(left) != 1 || left[0].Content != "never seen" {
		t.Fatalf("undelivered = %+v", left)
	}
	if _, ok := s.Interject("after end"); ok {
		t.Fatal("a finished turn must refuse")
	}
}

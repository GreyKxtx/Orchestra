package state

import "testing"

func TestReasoningSplitter_SingleChunk(t *testing.T) {
	var s ReasoningSplitter
	r, m := s.Feed("<think>analyzing</think>final answer")
	if r != "analyzing" {
		t.Errorf("reasoning: want %q, got %q", "analyzing", r)
	}
	if m != "final answer" {
		t.Errorf("message: want %q, got %q", "final answer", m)
	}
	if s.InThink {
		t.Errorf("InThink should be false after close")
	}
	if s.Carry != "" {
		t.Errorf("Carry should be empty, got %q", s.Carry)
	}
}

func TestReasoningSplitter_SplitTag(t *testing.T) {
	var s ReasoningSplitter
	r1, m1 := s.Feed("hello <th")
	if r1 != "" || m1 != "hello " {
		t.Errorf("first chunk: r=%q m=%q (carry should hold '<th')", r1, m1)
	}
	if s.Carry != "<th" {
		t.Errorf("Carry: want %q, got %q", "<th", s.Carry)
	}
	r2, m2 := s.Feed("ink>plan</think> reply")
	if r2 != "plan" {
		t.Errorf("reasoning: want %q, got %q", "plan", r2)
	}
	if m2 != " reply" {
		t.Errorf("message: want %q, got %q", " reply", m2)
	}
}

func TestReasoningSplitter_TagAcrossManyChunks(t *testing.T) {
	var s ReasoningSplitter
	full := "before <think>cot stream</think> after"
	chunks := []string{"bef", "ore <th", "ink>cot ", "stream</thin", "k> aft", "er"}
	var totalR, totalM string
	for _, c := range chunks {
		r, m := s.Feed(c)
		totalR += r
		totalM += m
	}
	if totalR != "cot stream" {
		t.Errorf("reasoning aggregate: want %q, got %q (full input %q)", "cot stream", totalR, full)
	}
	if totalM != "before  after" {
		t.Errorf("message aggregate: want %q, got %q", "before  after", totalM)
	}
}

func TestReasoningSplitter_Reset(t *testing.T) {
	var s ReasoningSplitter
	s.Feed("<think>partial")
	if !s.InThink {
		t.Fatalf("expected InThink=true after opening tag")
	}
	s.Reset()
	if s.InThink || s.Carry != "" {
		t.Errorf("Reset did not clear state: InThink=%v Carry=%q", s.InThink, s.Carry)
	}
}

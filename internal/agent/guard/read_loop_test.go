package guard

import "testing"

// A small model re-emits the same call with whatever whitespace and key order
// its sampler happens to produce. Keying the guard on raw bytes meant one
// stray space defeated it entirely, which is how the same file reached the
// prompt half a dozen times.
func TestCallKey_SeesThroughFormattingOfTheSameArguments(t *testing.T) {
	same := [][]byte{
		[]byte(`{"path":"README.md"}`),
		[]byte(`{"path": "README.md"}`),
		[]byte("{\n  \"path\": \"README.md\"\n}"),
	}
	want := callKey("read", same[0])
	for i, in := range same[1:] {
		if got := callKey("read", in); got != want {
			t.Fatalf("variant %d keyed differently: %q vs %q", i+1, got, want)
		}
	}
	// Key order is not meaning either.
	a := callKey("read", []byte(`{"path":"a.go","limit":20}`))
	b := callKey("read", []byte(`{"limit":20,"path":"a.go"}`))
	if a != b {
		t.Fatalf("key order changed the identity: %q vs %q", a, b)
	}
	// Different arguments must still be different calls.
	if callKey("read", []byte(`{"path":"a.go"}`)) == callKey("read", []byte(`{"path":"b.go"}`)) {
		t.Fatal("two different files must not share a key")
	}
	// Input that is not JSON at all still has to key stably.
	if callKey("read", []byte("not json")) != callKey("read", []byte("not json")) {
		t.Fatal("a non-JSON input must key stably")
	}
}

func repeatRead(cb *CircuitBreaker, n int) int {
	input := []byte(`{"path":"README.md"}`)
	ran := 0
	for i := 0; i < n; i++ {
		if cb.IsReadOnlyBlocked("read", input) {
			break
		}
		cb.RecordReadOnlyCall("read", input)
		ran++
	}
	return ran
}

// On a roomy window a few re-reads are affordable and sometimes right. On a
// 16k window they are fatal: three copies of one file can be a third of
// everything the model is allowed to see.
func TestReadOnlyRepeats_TightenOnASmallWindow(t *testing.T) {
	big := NewCircuitBreaker(2, 6, 6, 3)
	big.SetContextWindow(200000)
	if got := repeatRead(big, 10); got != readOnlyBlockRepeats {
		t.Fatalf("a large window keeps the standard budget: ran %d, want %d", got, readOnlyBlockRepeats)
	}

	small := NewCircuitBreaker(2, 6, 6, 3)
	small.SetContextWindow(16384)
	got := repeatRead(small, 10)
	if got >= readOnlyBlockRepeats {
		t.Fatalf("a small window must stop sooner than %d: ran %d", readOnlyBlockRepeats, got)
	}
	if got < 2 {
		t.Fatalf("one honest retry must still be allowed: ran %d", got)
	}
}

// An unset window must not change behaviour: callers that never learned the
// model's size keep the limits they always had.
func TestReadOnlyRepeats_UnknownWindowKeepsTheDefaults(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	if got := repeatRead(cb, 10); got != readOnlyBlockRepeats {
		t.Fatalf("ran %d, want the default %d", got, readOnlyBlockRepeats)
	}
}

// The warning has to arrive before the block, or the model is cut off without
// ever being told why.
func TestReadOnlyRepeats_WarnsBeforeBlocking(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	cb.SetContextWindow(16384)
	input := []byte(`{"path":"README.md"}`)
	var warned bool
	for i := 0; i < 10; i++ {
		if cb.IsReadOnlyBlocked("read", input) {
			break
		}
		if hint := cb.RecordReadOnlyCall("read", input); hint != "" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("the model must be told it is repeating itself before the call is refused")
	}
}

// Compaction may have dropped a result the model needs again, so it earns one
// repeat back — not a clean slate. Clearing the counters outright meant that on
// a small window, where compaction fires every few steps, the guard was reset
// faster than it could ever trip.
func TestCompaction_ForgivesOneRepeatRatherThanAllOfThem(t *testing.T) {
	cb := NewCircuitBreaker(2, 6, 6, 3)
	cb.SetContextWindow(16384)
	input := []byte(`{"path":"README.md"}`)

	ran := repeatRead(cb, 10)
	if !cb.IsReadOnlyBlocked("read", input) {
		t.Fatalf("the guard must be blocking after %d reads", ran)
	}

	cb.ForgiveReadOnlyCallsAfterCompaction()
	if cb.IsReadOnlyBlocked("read", input) {
		t.Fatal("one re-read must be allowed after compaction")
	}
	cb.RecordReadOnlyCall("read", input)
	if !cb.IsReadOnlyBlocked("read", input) {
		t.Fatal("and the guard must close again straight after it")
	}

	// A call seen only once is simply forgotten, so nothing lingers forever.
	fresh := NewCircuitBreaker(2, 6, 6, 3)
	fresh.RecordReadOnlyCall("ls", []byte(`{"path":"."}`))
	fresh.ForgiveReadOnlyCallsAfterCompaction()
	if fresh.readOnlyCallKeys[callKey("ls", []byte(`{"path":"."}`))] != 0 {
		t.Fatal("a single earlier call must not stay on the books")
	}
}
